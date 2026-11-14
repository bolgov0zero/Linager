<script lang="ts" module>
  export type TermPhase = 'connecting' | 'open' | 'closed';

  /** WebSocket close code the server uses when the per-server terminal limit is reached. */
  export const TERM_LIMIT_CODE = 4029;
</script>

<script lang="ts">
  import { openStream } from '../../api';
  import { tr, t } from '../../i18n';
  import { showToast } from '../toast';

  // The xterm screen and its socket, shared by the server-page tab and the floating windows.
  let {
    serverId,
    phase = $bindable('connecting'),
    onlimit,
  }: {
    serverId: string;
    phase?: TermPhase;
    /** Called when the server refused the session because of the terminal limit. */
    onlimit?: () => void;
  } = $props();

  // The terminal is always dark, like the design mockup; the palette matches the dark theme tokens.
  const TERM_THEME = {
    background: '#0f1218',
    foreground: '#e6e9ef',
    cursor: '#7d9cff',
    selectionBackground: '#3b4455',
  };

  let host = $state<HTMLDivElement | null>(null);
  let reason = $state<string | null>(null);
  let attempt = $state(0);

  // Non-reactive handle to the live session, used by the exported actions.
  let current: { close: () => void; copy: () => void; paste: () => void; focus: () => void } | null = null;

  export const focus = () => current?.focus();
  export const copy = () => current?.copy();
  export const paste = () => current?.paste();
  export const close = () => current?.close();

  /** Copies text with the async Clipboard API (secure contexts) or the legacy execCommand fallback (plain HTTP). */
  async function copyText(text: string): Promise<boolean> {
    try {
      if (window.isSecureContext && navigator.clipboard?.writeText) {
        await navigator.clipboard.writeText(text);
        return true;
      }
    } catch {
      // permission denied - fall through to the legacy path
    }
    const area = document.createElement('textarea');
    area.value = text;
    area.setAttribute('readonly', '');
    area.style.position = 'fixed';
    area.style.opacity = '0';
    document.body.appendChild(area);
    area.select();
    let ok = false;
    try {
      ok = document.execCommand('copy');
    } catch {
      ok = false;
    }
    area.remove();
    return ok;
  }

  // One terminal + one socket per mount/reconnect. Torn down on unmount or reconnect.
  // xterm is loaded on demand so it stays out of the main bundle.
  $effect(() => {
    const el = host;
    const id = serverId;
    void attempt;
    if (!el) return;

    let disposed = false;
    let teardown: (() => void) | undefined;

    (async () => {
      const [{ Terminal }, { FitAddon }] = await Promise.all([
        import('@xterm/xterm'),
        import('@xterm/addon-fit'),
        import('@xterm/xterm/css/xterm.css'),
      ]);
      if (disposed) return;

      el.replaceChildren();
      const fontFamily = getComputedStyle(document.documentElement).getPropertyValue('--font-body').trim();
      const term = new Terminal({
        cursorBlink: true,
        fontSize: 13,
        fontFamily: fontFamily || 'monospace',
        theme: TERM_THEME,
        scrollback: 2000,
      });
      const fit = new FitAddon();
      term.loadAddon(fit);
      term.open(el);
      fit.fit();

      const copySelection = async () => {
        if (!term.hasSelection()) return;
        showToast(t((await copyText(term.getSelection())) ? 'term.copied' : 'term.copyFail'));
      };
      // Ctrl+C copies only when text is selected (otherwise it stays SIGINT); Ctrl+Shift+C always copies.
      // Paste keys return false so xterm ignores them and the browser's native paste event reaches xterm,
      // which works on plain HTTP too (no Clipboard API needed). Cmd+C is handled natively by xterm.
      term.attachCustomKeyEventHandler((e: KeyboardEvent) => {
        if (e.type !== 'keydown') return true;
        const key = e.key.toLowerCase();
        if (e.ctrlKey && !e.metaKey && key === 'c' && (e.shiftKey || term.hasSelection())) {
          void copySelection();
          return false;
        }
        if ((e.ctrlKey || e.metaKey) && key === 'v') return false;
        return true;
      });

      const encoder = new TextEncoder();
      const socket = openStream(id, 'terminal', { cols: term.cols, rows: term.rows });
      let active = true;
      phase = 'connecting';
      reason = null;

      const sendResize = (cols: number, rows: number) => {
        if (socket.readyState === WebSocket.OPEN) socket.send(JSON.stringify({ resize: { cols, rows } }));
      };

      socket.onopen = () => {
        if (!active) return;
        phase = 'open';
        sendResize(term.cols, term.rows);
      };

      socket.onmessage = (event: MessageEvent<unknown>) => {
        if (!active) return;
        if (typeof event.data === 'string') {
          // Text frames carry {error} or {closed} from the agent; the close event follows.
          try {
            const msg = JSON.parse(event.data) as { error?: unknown; closed?: unknown };
            if (typeof msg.error === 'string') reason = msg.error;
            else if (typeof msg.closed === 'string') reason = msg.closed;
          } catch {
            // not JSON - ignore
          }
          return;
        }
        term.write(new Uint8Array(event.data as ArrayBuffer));
      };

      // Covers handshake failures (501/503) too: the browser only reports a plain close.
      socket.onclose = (event: CloseEvent) => {
        if (!active) return;
        phase = 'closed';
        if (event.code === TERM_LIMIT_CODE) {
          reason = t('term.limit');
          showToast(reason);
          onlimit?.();
        }
      };

      const onData = term.onData((data) => {
        if (active && socket.readyState === WebSocket.OPEN) socket.send(encoder.encode(data));
      });
      const onResize = term.onResize(({ cols, rows }) => {
        if (active) sendResize(cols, rows);
      });
      // Window resizing reaches the PTY here: fit() changes cols/rows, onResize sends them.
      // A hidden (zero-size) host is skipped so a minimized window keeps its geometry.
      const observer = new ResizeObserver(() => {
        if (el.clientWidth > 0 && el.clientHeight > 0) fit.fit();
      });
      observer.observe(el);

      term.focus();
      current = {
        focus: () => term.focus(),
        close: () => socket.close(),
        copy: () => void copySelection(),
        paste: async () => {
          term.focus();
          if (window.isSecureContext && navigator.clipboard?.readText) {
            try {
              term.paste(await navigator.clipboard.readText());
              return;
            } catch {
              // permission denied - ask for the keyboard shortcut instead
            }
          }
          showToast(t('term.pasteHint'));
        },
      };

      teardown = () => {
        active = false;
        observer.disconnect();
        onData.dispose();
        onResize.dispose();
        socket.close();
        term.dispose();
        current = null;
      };
    })();

    return () => {
      disposed = true;
      teardown?.();
    };
  });
</script>

<div class="session">
  <!-- Clicking anywhere in the terminal panel (also outside the character grid) focuses the terminal. -->
  <div class="screen" role="presentation" bind:this={host} onpointerdown={() => current?.focus()}></div>
  {#if phase === 'closed'}
    <div class="closed">
      <p class="error" role="alert">{reason ?? $tr('term.closed')}</p>
      <button class="btn" type="button" onclick={() => attempt++}>{$tr('term.reconnect')}</button>
    </div>
  {/if}
</div>

<style>
  .session {
    display: flex;
    flex-direction: column;
    gap: 12px;
    min-width: 0;
    height: 100%;
  }

  .screen {
    flex: 1;
    min-height: 0;
    min-width: 0;
    padding: 8px;
    overflow: hidden;
    background: #0f1218;
    border: 2px solid var(--line);
  }

  .closed {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 12px;
  }
</style>
