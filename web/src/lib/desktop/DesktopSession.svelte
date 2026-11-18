<script lang="ts" module>
  export type DeskPhase = 'form' | 'connecting' | 'open' | 'closed';

  /** WebSocket close code of the server when a remote desktop of the computer is already open. */
  export const DESKTOP_LIMIT_CODE = 4030;

  /** The rdp client wants these numbers for the scale: fit the window, fill it, or show the real pixels. */
  export const SCALE = { fit: 1, full: 2, real: 3 } as const;
</script>

<script lang="ts">
  import { onDestroy } from 'svelte';
  import { desktopApi, desktopSocketUrl, type DesktopState } from '../../api';
  import { tr, t } from '../../i18n';
  import { showToast } from '../toast';

  /**
   * The remote desktop of a Windows computer: a login form, then the screen of the browser RDP client (IronRDP,
   * WebAssembly, loaded when it is first needed). The client talks RDCleanPath over a WebSocket to the panel, which
   * carries it through the agent to the Remote Desktop service of the computer. The password goes to the client
   * only; it is not stored and not sent to the panel in the clear.
   */
  let {
    serverId,
    address,
    phase = $bindable('form'),
    autoResize = $bindable(true),
    onended,
  }: {
    serverId: string;
    /** How the computer is called inside the connection (its address in the panel). */
    address: string;
    phase?: DeskPhase;
    /** Ask the computer to change its resolution when the window changes. */
    autoResize?: boolean;
    onended?: () => void;
  } = $props();

  const loginKey = () => `linager.desktop.login.${serverId}`;
  function savedLogin(): string {
    try {
      return localStorage.getItem(loginKey()) ?? '';
    } catch {
      return '';
    }
  }

  let login = $state(savedLogin());
  let password = $state('');
  let domain = $state('');
  let message = $state('');
  let hostState = $state<DesktopState | null>(null);
  let enabling = $state(false);
  let host = $state<HTMLDivElement | null>(null);

  // Handles of the live client; not reactive.
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  let ui: any = null;
  let element: HTMLElement | null = null;
  let resizeTimer: ReturnType<typeof setTimeout> | undefined;
  let observer: ResizeObserver | undefined;
  let gone = false;

  $effect(() => {
    if (phase !== 'form') return;
    void desktopApi.state(serverId).then((s) => (hostState = s)).catch(() => (hostState = null));
  });

  async function enable(): Promise<void> {
    enabling = true;
    try {
      hostState = await desktopApi.enable(serverId);
      showToast(t('desk.enabled'));
    } catch (e) {
      message = e instanceof Error ? e.message : t('common.genericError');
    } finally {
      enabling = false;
    }
  }

  /** The words for what the client reports when the connection fails. */
  function explain(error: unknown): string {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const e = error as any;
    let kind = -1;
    try {
      kind = typeof e?.kind === 'function' ? e.kind() : -1;
    } catch {
      kind = -1;
    }
    switch (kind) {
      case 1:
      case 2:
        return t('desk.err.logon');
      case 3:
        return t('desk.err.denied');
      case 4:
      case 5:
        return t('desk.err.proxy');
      case 6:
        return t('desk.err.negotiation');
    }
    const text = typeof e?.backtrace === 'function' ? String(e.backtrace()) : e instanceof Error ? e.message : String(e ?? '');
    return text.split('\n')[0] || t('common.genericError');
  }

  function size(): { width: number; height: number } {
    const w = host?.clientWidth || 1024;
    const h = host?.clientHeight || 700;
    const even = (n: number, lo: number, hi: number) => Math.min(hi, Math.max(lo, n - (n % 2)));
    return { width: even(w, 640, 4096), height: even(h, 480, 4096) };
  }

  export async function connect(): Promise<void> {
    if (!login.trim() || !password) return;
    message = '';
    phase = 'connecting';
    try {
      const [rdp] = await Promise.all([import('@devolutions/iron-remote-desktop-rdp'), import('@devolutions/iron-remote-desktop')]);
      if (gone || !host) return;
      await rdp.init('warn'); // loads the WebAssembly module
      if (gone || !host) return;
      element = document.createElement('iron-remote-desktop');
      element.setAttribute('scale', 'fit');
      element.setAttribute('verbose', 'false');
      element.setAttribute('flexcenter', 'true');
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      (element as any).module = rdp.Backend;
      const ready = new Promise<void>((resolve) => {
        element?.addEventListener('ready', (ev) => {
          ui = (ev as CustomEvent).detail?.irgUserInteraction ?? null;
          resolve();
        });
      });
      host.appendChild(element);
      await ready;
      ui.setEnableClipboard(true);
      ui.setEnableAutoClipboard(true);
      ui.onWarningCallback((w: string) => showToast(w));
      const config = ui
        .configBuilder()
        .withUsername(login.trim())
        .withPassword(password)
        .withServerDomain(domain.trim())
        .withDestination(`${address}:${hostState?.port ?? 3389}`)
        .withProxyAddress(desktopSocketUrl(serverId, login.trim()))
        .withAuthToken('linager') // the panel signs the person in by the session cookie of the same page
        .withDesktopSize(size())
        .withExtension(rdp.displayControl(true))
        .build();
      const info = await ui.connect(config);
      ui.setVisibility(true); // the screen of the client stays hidden until it is asked to show
      try {
        localStorage.setItem(loginKey(), login.trim());
      } catch {
        // storage unavailable: the login is just not remembered
      }
      password = '';
      phase = 'open';
      watchSize();
      void info.run().then(
        (end: { reason: () => string }) => finish(end.reason()),
        (err: unknown) => finish(explain(err)),
      );
    } catch (err) {
      cleanup();
      message = explain(err);
      phase = 'form';
    }
  }

  function finish(reason: string): void {
    if (gone) return;
    cleanup();
    message = reason;
    phase = 'closed';
    onended?.();
  }

  function cleanup(): void {
    observer?.disconnect();
    clearTimeout(resizeTimer);
    try {
      ui?.shutdown();
    } catch {
      // already closed
    }
    element?.remove();
    element = null;
    ui = null;
  }

  /** The window changed size: the computer changes its resolution with it (when that is switched on). */
  function watchSize(): void {
    if (!host) return;
    observer = new ResizeObserver(() => {
      if (!autoResize) return;
      clearTimeout(resizeTimer);
      resizeTimer = setTimeout(() => {
        const s = size();
        try {
          ui?.resize(s.width, s.height);
        } catch {
          // the computer does not support it
        }
      }, 350);
    });
    observer.observe(host);
  }

  // ---- what the toolbar can do ----

  export function disconnect(): void {
    cleanup();
    message = '';
    phase = 'form';
  }

  export function focus(): void {
    element?.focus();
  }

  export function ctrlAltDel(): void {
    ui?.ctrlAltDel();
    focus();
  }

  export function winKey(): void {
    ui?.metaKey();
    focus();
  }

  /** Presses and releases keys together, named by KeyboardEvent.code. The client reads them from the page's key events. */
  export function combo(codes: string[]): void {
    if (!ui) return;
    focus();
    const send = (type: 'keydown' | 'keyup', code: string, held: Set<string>) =>
      window.dispatchEvent(
        new KeyboardEvent(type, {
          code,
          key: code.replace(/^(Key|Digit)/, '').replace(/(Left|Right)$/, ''),
          ctrlKey: held.has('ControlLeft'),
          altKey: held.has('AltLeft'),
          shiftKey: held.has('ShiftLeft'),
          metaKey: held.has('MetaLeft'),
          bubbles: true,
          cancelable: true,
        }),
      );
    const held = new Set<string>();
    for (const c of codes) {
      held.add(c);
      send('keydown', c, held);
    }
    for (const c of [...codes].reverse()) {
      send('keyup', c, held);
      held.delete(c);
    }
  }

  export function setScale(kind: keyof typeof SCALE): void {
    ui?.setScale(SCALE[kind]);
  }

  export function fullscreen(): void {
    void host?.requestFullscreen?.();
  }

  /** The local clipboard to the computer (the browser asks for permission). */
  export async function sendClipboard(): Promise<void> {
    try {
      await ui?.sendClipboardData();
    } catch (e) {
      showToast(explain(e));
    }
  }

  /** The clipboard of the computer to the local one. */
  export async function saveClipboard(): Promise<void> {
    try {
      await ui?.saveRemoteClipboardData();
      showToast(t('desk.clipboardSaved'));
    } catch (e) {
      showToast(explain(e));
    }
  }

  export const clipboardAvailable = (): boolean => typeof navigator !== 'undefined' && !!navigator.clipboard?.readText;

  onDestroy(() => {
    gone = true;
    cleanup();
  });

  const needsEnable = $derived(hostState !== null && (!hostState.enabled || !hostState.listening));
</script>

<div class="desk">
  <div class="screen" bind:this={host} class:live={phase === 'open' || phase === 'connecting'}></div>

  {#if phase !== 'open'}
    <form
      class="form"
      onsubmit={(e) => {
        e.preventDefault();
        void connect();
      }}>
      <h3>{$tr('desk.title')}</h3>
      {#if phase === 'connecting'}
        <p class="muted">{$tr('desk.connecting')}</p>
      {:else}
        {#if hostState && !hostState.supported}
          <p class="warn">{$tr('desk.unsupported', { edition: hostState.edition })}</p>
        {:else if needsEnable}
          <p class="warn">{hostState && !hostState.enabled ? $tr('desk.off') : $tr('desk.notListening')}</p>
          <button class="btn" type="button" disabled={enabling} onclick={() => void enable()}>{$tr('desk.enable')}</button>
        {/if}
        <label>
          <span>{$tr('desk.login')}</span>
          <input type="text" bind:value={login} autocomplete="username" spellcheck="false" autocapitalize="off" />
        </label>
        <label>
          <span>{$tr('desk.password')}</span>
          <input type="password" bind:value={password} autocomplete="current-password" />
        </label>
        <label>
          <span>{$tr('desk.domain')} <i class="muted">{$tr('desk.optional')}</i></span>
          <input type="text" bind:value={domain} spellcheck="false" autocapitalize="off" />
        </label>
        {#if message}<p class="err" role="alert">{message}</p>{/if}
        <button class="btn primary" type="submit" disabled={!login.trim() || !password}>{phase === 'closed' ? $tr('desk.reconnect') : $tr('desk.connect')}</button>
        <p class="small muted">{$tr('desk.note')}</p>
      {/if}
    </form>
  {/if}
</div>

<style>
  .desk {
    position: relative;
    flex: 1;
    width: 100%;
    height: 100%;
    min-height: 0;
    min-width: 0;
    display: flex;
    background: #0b0e13;
  }

  .screen {
    flex: 1;
    min-width: 0;
    min-height: 0;
    display: none;
    overflow: hidden;
  }

  .screen.live {
    display: block;
  }

  .screen :global(iron-remote-desktop) {
    display: block;
    width: 100%;
    height: 100%;
  }

  .form {
    position: absolute;
    inset: 0;
    margin: auto;
    width: min(324px, calc(100% - 22px));
    height: fit-content;
    max-height: calc(100% - 22px);
    overflow: auto;
    display: flex;
    flex-direction: column;
    gap: 9px;
    padding: 14px;
    background: var(--surface);
    border: 2px solid var(--line);
  }

  .form h3 {
    margin: 0;
  }

  label {
    display: flex;
    flex-direction: column;
    gap: 4px;
    font-size: 12px;
  }

  input {
    padding: 7px 9px;
    font: inherit;
    color: var(--ink);
    background: var(--bg);
    border: 2px solid var(--line);
  }

  .warn {
    margin: 0;
    padding: 7px 9px;
    border: 2px solid var(--warn);
    color: var(--warn);
    font-size: 12px;
  }

  .err {
    margin: 0;
    color: var(--crit);
    font-size: 12px;
    overflow-wrap: anywhere;
  }

  p {
    margin: 0;
  }
</style>
