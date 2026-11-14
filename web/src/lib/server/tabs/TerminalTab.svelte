<script lang="ts">
  import { openStream, type ServerDetail } from '../../../api';
  import { tr } from '../../../i18n';

  let { server }: { server: ServerDetail } = $props();

  type Phase = 'connecting' | 'open' | 'closed';

  // The terminal is always dark, like the design mockup; the palette matches the dark theme tokens.
  const TERM_THEME = {
    background: '#0f1218',
    foreground: '#e6e9ef',
    cursor: '#7d9cff',
    selectionBackground: '#3b4455',
  };

  let host = $state<HTMLDivElement | null>(null);
  let phase = $state<Phase>('connecting');
  let reason = $state<string | null>(null);
  let attempt = $state(0);

  // Non-reactive handle to the live session, used by the close button.
  let current: { close: () => void } | null = null;

  // One terminal + one socket per mount/reconnect. Torn down on unmount, offline, or reconnect.
  // xterm is loaded on demand so it stays out of the main bundle.
  $effect(() => {
    const el = host;
    const online = server.online;
    const id = server.id;
    void attempt;
    if (!el || !online) return;

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
      socket.onclose = () => {
        if (!active) return;
        phase = 'closed';
      };

      const onData = term.onData((data) => {
        if (active && socket.readyState === WebSocket.OPEN) socket.send(encoder.encode(data));
      });
      const onResize = term.onResize(({ cols, rows }) => {
        if (active) sendResize(cols, rows);
      });
      const observer = new ResizeObserver(() => fit.fit());
      observer.observe(el);

      current = { close: () => socket.close() };

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

  const statusText = $derived(
    phase === 'connecting' ? $tr('term.connecting') : phase === 'open' ? $tr('term.open') : $tr('term.closed'),
  );
</script>

<div class="term">
  <div class="head">
    <p class="meta">
      <span class="dot" class:on={server.online && phase === 'open'} aria-hidden="true"></span>
      <span>{$tr('term.header', { name: server.name })}</span>
      {#if server.online}
        <span class="muted">· {statusText}</span>
      {/if}
    </p>
    {#if server.online && phase !== 'closed'}
      <button class="btn" type="button" onclick={() => current?.close()}>{$tr('term.close')}</button>
    {/if}
  </div>

  {#if !server.online}
    <p class="note">{$tr('term.offline')}</p>
  {:else}
    <div class="screen" bind:this={host}></div>
    {#if phase === 'closed'}
      <div class="closed">
        <p class="error" role="alert">{reason ?? $tr('term.closed')}</p>
        <button class="btn" type="button" onclick={() => attempt++}>{$tr('term.reconnect')}</button>
      </div>
    {/if}
  {/if}
</div>

<style>
  .term {
    display: flex;
    flex-direction: column;
    gap: 12px;
    min-width: 0;
  }

  .head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
  }

  .meta {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
    margin: 0;
    font-size: 12px;
  }

  .dot {
    flex: none;
    width: 10px;
    height: 10px;
    border: 2px solid var(--line);
    background: var(--muted);
  }

  .dot.on {
    background: var(--good);
  }

  .screen {
    height: 420px;
    min-width: 0;
    padding: 8px;
    overflow: hidden;
    background: #0f1218;
    border: 2px solid var(--line);
  }

  .note {
    margin: 0;
    padding: 24px 0;
    text-align: center;
    color: var(--muted);
  }

  .closed {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 12px;
  }

  @media (max-width: 600px) {
    .screen {
      height: 320px;
    }
  }
</style>
