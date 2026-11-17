<script lang="ts" module>
  export type TermPhase = 'connecting' | 'open' | 'closed';

  /** WebSocket close code the server uses when the per-server terminal limit is reached. */
  export const TERM_LIMIT_CODE = 4029;
</script>

<script lang="ts">
  import { openStream } from '../../api';
  import { tr, t } from '../../i18n';
  import { showToast } from '../toast';
  import { get } from 'svelte/store';
  import { isPhone } from '../ui/viewport';
  import KeyPanel from './KeyPanel.svelte';
  import QuickKeys from './QuickKeys.svelte';
  import { viewBox } from './vv';
  import { ctrlChar, sequenceFor } from './keys';

  // The xterm screen and its socket, shared by the server-page tab and the floating windows.
  let {
    serverId,
    asUser = '',
    phase = $bindable('connecting'),
    keys = $bindable(false),
    onlimit,
  }: {
    serverId: string;
    /** Open the shell as this account instead of root. */
    asUser?: string;
    phase?: TermPhase;
    /** Show the on-screen key panel above the screen. */
    keys?: boolean;
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
  // The terminal has focus: on a phone that means the soft keyboard is up and the quick key row shows above it.
  let focused = $state(false);

  // Non-reactive handle to the live session, used by the exported actions.
  let current: {
    close: () => void;
    copy: () => void;
    paste: () => void;
    focus: () => void;
    blur: () => void;
    send: (data: string) => void;
    appCursor: () => boolean;
    insert: (text: string, run: boolean) => void;
  } | null = null;

  // Ctrl and Alt from the key panel stay latched until the next key (from the panel or a real keyboard).
  let ctrl = $state(false);
  let alt = $state(false);

  function panelKey(name: string): void {
    if (!current) return;
    // "ctrl:c" is a ready shortcut and ignores the latch.
    const seq = name.startsWith('ctrl:') ? (ctrlChar(name.slice(5)) ?? '') : sequenceFor(name, { ctrl, alt }, current.appCursor());
    ctrl = false;
    alt = false;
    if (seq) current.send(seq);
    current.focus();
  }

  function toggleMod(mod: 'ctrl' | 'alt'): void {
    if (mod === 'ctrl') ctrl = !ctrl;
    else alt = !alt;
  }

  export const focus = () => current?.focus();
  export const blur = () => current?.blur();
  export const copy = () => current?.copy();
  export const paste = () => current?.paste();
  export const close = () => current?.close();
  /** Types text into the terminal as a paste; with run it is executed (Enter) right after. */
  export const insert = (text: string, run: boolean) => current?.insert(text, run);

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
    const as = asUser; // a new account starts a new shell
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
        fontSize: get(isPhone) ? 12 : 13,
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
      const socket = openStream(id, 'terminal', as ? { cols: term.cols, rows: term.rows, user: as } : { cols: term.cols, rows: term.rows });
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

      const send = (data: string) => {
        if (active && socket.readyState === WebSocket.OPEN) socket.send(encoder.encode(data));
      };
      const onData = term.onData((data) => {
        // A latched Ctrl or Alt from the key panel applies to the next character typed on the real keyboard.
        if ((ctrl || alt) && data.length === 1) {
          let out = ctrl ? (ctrlChar(data) ?? data) : data;
          if (alt) out = `\x1b${out}`;
          ctrl = false;
          alt = false;
          send(out);
          return;
        }
        send(data);
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
        blur: () => term.blur(),
        close: () => socket.close(),
        send,
        appCursor: () => term.modes.applicationCursorKeysMode,
        insert: (text, run) => {
          term.focus();
          term.paste(text); // a paste, so multi-line text stays one block where the shell knows bracketed paste
          if (run) send('\r');
        },
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
  {#if keys}
    <KeyPanel {ctrl} {alt} onkey={panelKey} ontoggle={toggleMod} />
  {/if}
  <!-- Clicking anywhere in the terminal panel (also outside the character grid) focuses the terminal. -->
  <div
    class="screen"
    role="presentation"
    onpointerdown={() => current?.focus()}
    onfocusin={() => (focused = true)}
    onfocusout={(e) => {
      if (!e.currentTarget.contains(e.relatedTarget as Node | null)) focused = false;
    }}
  >
    <!-- The frame and padding are on .screen: fit() takes the height of the element xterm sits in, and a
         border-box host with padding would make it count rows that do not fit (the last line was cut off). -->
    <div class="grid" bind:this={host}></div>
  </div>
  {#if phase === 'closed'}
    <div class="closed">
      <p class="error" role="alert">{reason ?? $tr('term.closed')}</p>
      <button class="btn" type="button" onclick={() => attempt++}>{$tr('term.reconnect')}</button>
    </div>
  {/if}
  {#if $isPhone && phase === 'open' && (focused || $viewBox.kbd || keys)}
    <QuickKeys
      {ctrl}
      {alt}
      more={keys}
      onkey={panelKey}
      ontoggle={toggleMod}
      onmore={() => (keys = !keys)}
      onhide={() => current?.blur()}
    />
  {/if}
</div>

<style>
  .session {
    display: flex;
    flex-direction: column;
    gap: 8px;
    min-width: 0;
    height: 100%;
  }

  .screen {
    flex: 1;
    min-height: 0;
    min-width: 0;
    display: flex;
    padding: 8px;
    overflow: hidden;
    background: #0f1218;
    border: 2px solid var(--line);
  }

  .grid {
    flex: 1;
    min-width: 0;
    min-height: 0;
    overflow: hidden;
  }

  .closed {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 12px;
  }

  /* Phones: the screen takes all the room the owner gives it, no frame; the key panel and the quick row sit under it. */
  @media (max-width: 720px) {
    .session {
      gap: 0;
    }

    .session > :global(.keys) {
      order: 2;
      border-width: 2px 0 0;
    }

    .session > :global(.quick) {
      order: 3;
    }

    .screen {
      order: 1;
      padding: 4px 6px;
      border-width: 0;
    }

    /* The hidden input of xterm: below 16 px iOS would zoom the page when the keyboard opens. */
    .screen :global(.xterm-helper-textarea) {
      font-size: 16px;
    }

    .closed {
      order: 1;
      padding: 8px 12px;
      background: var(--surface);
      border-top: 2px solid var(--line);
    }
  }
</style>
