<script lang="ts" module>
  export type AssistPhase = 'connecting' | 'open' | 'closed';
  export type AssistScale = 'fit' | 'real';

  /** What the computer reports about the picture; null until the first Hello. */
  export interface AssistInfo {
    w: number;
    h: number;
    user: string;
    /** Frames and traffic of the last second (the Stats message); zero until the first one. */
    fps: number;
    kbps: number;
    q: number;
    /** "default" normally; "winlogon" on the lock, sign-in or UAC screen (see isSecureDesktop). */
    desktop: string;
    /** The monitors of the computer (primary first; empty when it did not say) and the picked one, -1 for all as one picture. */
    monitors: Monitor[];
    monitor: number;
  }
</script>

<script lang="ts">
  import { untrack } from 'svelte';
  import { assistantSocketUrl } from '../../api';
  import { tr, t } from '../../i18n';
  import { showToast } from '../toast';
  import {
    ASSIST_LIMIT_CODE,
    MSG_CLIP,
    MSG_END,
    MSG_HELLO,
    MSG_STATS,
    MSG_TILES,
    WheelNotches,
    cleanMonitors,
    encodeClipGet,
    encodeClipSet,
    encodeInput,
    encodeMonitor,
    parseTiles,
    type Hello,
    type Monitor,
    type Stats,
  } from './protocol';
  import { scanKey, type ScanKey } from './scancodes';
  import { assistSettings, refreshAssistSettings } from './settings';

  /**
   * The screen of a Windows session in the browser: a canvas that gets the picture as JPEG tiles over a WebSocket of
   * the panel (which carries it through the agent) and sends the mouse and the keys back as scan codes. The session
   * is 'console' (what a person sits in front of) or the number of a Windows session; it opens when the component
   * appears and again when the session changes.
   */
  let {
    serverId,
    session,
    phase = $bindable('connecting'),
    info = $bindable(null),
  }: {
    serverId: string;
    session: string;
    phase?: AssistPhase;
    info?: AssistInfo | null;
  } = $props();

  let canvas = $state<HTMLCanvasElement | null>(null);
  let host = $state<HTMLDivElement | null>(null);
  let message = $state('');
  let mode = $state<AssistScale>('fit');
  let box = $state({ w: 0, h: 0 });
  let size = $state({ w: 0, h: 0 });

  // Handles of the live connection; not reactive.
  let socket: WebSocket | null = null;
  let generation = 0;
  let queue: Promise<void> = Promise.resolve();
  const pressedKeys = new Map<string, ScanKey>();
  const pressedButtons = new Set<number>();
  let last = { x: 0, y: 0 };
  let pendingMove: { x: number; y: number } | null = null;
  let frame = 0;
  const wheel = new WheelNotches();
  /** Set while the clipboard of the computer is asked for: only an answer to that is put into the local clipboard. */
  let clipWanted = 0;

  // The pointer is part of the picture unless the settings say it is not drawn there; then the local one is shown (no lag).
  const hideCursor = $derived($assistSettings[serverId]?.hide_cursor === true);

  const factor = $derived(mode === 'real' || box.w <= 0 || box.h <= 0 || size.w <= 0 ? 1 : Math.min(box.w / size.w, box.h / size.h));
  const cssW = $derived(Math.max(1, Math.floor(size.w * factor)));
  const cssH = $derived(Math.max(1, Math.floor(size.h * factor)));

  // ---- the connection ----

  function connect(): void {
    shutdown();
    const mine = ++generation;
    message = '';
    info = null;
    clipWanted = 0;
    size = { w: 0, h: 0 };
    phase = 'connecting';
    const ws = new WebSocket(assistantSocketUrl(serverId, session));
    ws.binaryType = 'arraybuffer';
    socket = ws;
    queue = Promise.resolve();
    ws.onmessage = (ev) => {
      if (mine !== generation || !(ev.data instanceof ArrayBuffer)) return;
      const data = new Uint8Array(ev.data);
      // one chain: a frame is drawn completely before the next one is looked at
      queue = queue.then(() => handle(mine, data)).catch(() => undefined);
    };
    ws.onclose = (ev) => {
      if (mine !== generation) return;
      socket = null;
      releaseAll(false);
      if (phase !== 'closed') end(ev.code === ASSIST_LIMIT_CODE ? t('assist.limit') : ev.code === 1000 ? t('assist.ended') : t('assist.lost'));
    };
  }

  /** Closes the socket without a word; what a message handler still has in the air is dropped. */
  function shutdown(): void {
    generation++;
    cancelAnimationFrame(frame);
    pendingMove = null;
    const ws = socket;
    socket = null;
    if (ws) {
      ws.onmessage = ws.onclose = null;
      try {
        ws.close(1000);
      } catch {
        // already closed
      }
    }
    pressedKeys.clear();
    pressedButtons.clear();
  }

  function end(reason: string): void {
    message = reason;
    phase = 'closed';
    info = null;
  }

  async function handle(mine: number, data: Uint8Array): Promise<void> {
    if (mine !== generation || data.length === 0) return;
    const body = data.subarray(1);
    switch (data[0]) {
      case MSG_HELLO: {
        const hello = json<Hello>(body);
        if (!hello || !(hello.w > 0) || !(hello.h > 0)) return;
        size = { w: hello.w, h: hello.h };
        if (canvas) {
          canvas.width = hello.w; // resizing clears the picture; the tiles that follow fill it again
          canvas.height = hello.h;
        }
        info = {
          w: hello.w,
          h: hello.h,
          user: hello.user ?? '',
          fps: info?.fps ?? 0,
          kbps: info?.kbps ?? 0,
          q: info?.q ?? 0,
          desktop: info?.desktop ?? 'default',
          monitors: cleanMonitors(hello.monitors),
          monitor: Number.isInteger(hello.monitor) ? (hello.monitor as number) : -1,
        };
        phase = 'open';
        return;
      }
      case MSG_TILES:
        await draw(mine, body);
        return;
      case MSG_STATS: {
        const s = json<Stats>(body);
        if (s && info) {
          info = { ...info, fps: Number(s.fps) || 0, kbps: Number(s.kbps) || 0, q: Number(s.q) || 0, desktop: typeof s.desktop === 'string' && s.desktop ? s.desktop : 'default' };
        }
        return;
      }
      case MSG_CLIP:
        await takeClip(json<{ text?: string }>(body)?.text);
        return;
      case MSG_END:
        end(json<{ reason?: string }>(body)?.reason || t('assist.ended'));
        return;
    }
  }

  /** The clipboard of the computer came (the answer to getClipboard): it goes into the local clipboard. */
  async function takeClip(text: string | undefined): Promise<void> {
    if (Date.now() > clipWanted) return; // nobody asked: the clipboard of the person is not touched
    clipWanted = 0;
    if (typeof text !== 'string') return;
    if (text === '') {
      showToast(t('assist.clipEmpty'));
      return;
    }
    try {
      await navigator.clipboard.writeText(text);
      showToast(t('desk.clipboardSaved'));
    } catch {
      showToast(t('assist.clipDenied'));
    }
  }

  function json<T>(bytes: Uint8Array): T | null {
    try {
      return JSON.parse(new TextDecoder().decode(bytes)) as T;
    } catch {
      return null;
    }
  }

  async function draw(mine: number, body: Uint8Array): Promise<void> {
    const ctx = canvas?.getContext('2d');
    const tiles = parseTiles(body);
    if (!ctx || !tiles || size.w === 0) return; // the picture before the first Hello has no size to go to
    const bitmaps = await Promise.all(
      tiles.map((tile) => createImageBitmap(new Blob([tile.jpeg as BlobPart], { type: 'image/jpeg' })).catch(() => null)),
    );
    for (let i = 0; i < bitmaps.length; i++) {
      const bitmap = bitmaps[i];
      if (!bitmap) continue;
      if (mine === generation) ctx.drawImage(bitmap, tiles[i].x, tiles[i].y);
      bitmap.close();
    }
  }

  // The session or the computer changed (or the window appeared): connect again.
  $effect(() => {
    void serverId;
    void session;
    untrack(connect);
    return shutdown;
  });

  // The setting of the pointer is needed as soon as the picture is there (the line under the screen reads it again from time to time).
  $effect(() => {
    if (phase === 'open') void refreshAssistSettings(serverId);
  });

  // ---- input ----

  function send(event: Record<string, unknown>): void {
    if (socket?.readyState === WebSocket.OPEN && phase === 'open') socket.send(encodeInput(event));
  }

  function remote(e: { clientX: number; clientY: number }): { x: number; y: number } {
    const r = canvas?.getBoundingClientRect();
    if (!r || r.width === 0 || r.height === 0) return last;
    const x = Math.max(0, Math.min(size.w - 1, Math.round(((e.clientX - r.left) * size.w) / r.width)));
    const y = Math.max(0, Math.min(size.h - 1, Math.round(((e.clientY - r.top) * size.h) / r.height)));
    last = { x, y };
    return last;
  }

  function flushMove(): void {
    frame = 0;
    if (!pendingMove) return;
    send({ t: 'm', x: pendingMove.x, y: pendingMove.y });
    pendingMove = null;
  }

  function onpointermove(e: PointerEvent): void {
    pendingMove = remote(e);
    if (!frame) frame = requestAnimationFrame(flushMove);
  }

  function onpointerdown(e: PointerEvent): void {
    e.preventDefault();
    canvas?.focus();
    canvas?.setPointerCapture(e.pointerId);
    pressedButtons.add(e.button);
    const p = remote(e);
    send({ t: 'b', b: e.button, d: true, x: p.x, y: p.y });
  }

  function onpointerup(e: PointerEvent): void {
    if (!pressedButtons.delete(e.button)) return;
    const p = remote(e);
    send({ t: 'b', b: e.button, d: false, x: p.x, y: p.y });
  }

  function onwheel(e: WheelEvent): void {
    e.preventDefault();
    const n = wheel.push(e.deltaX, e.deltaY, e.deltaMode);
    if (n.dx !== 0 || n.dy !== 0) send({ t: 'w', dx: n.dx, dy: n.dy });
  }

  const isModifier = (code: string): boolean => /^(Control|Shift|Alt|Meta)(Left|Right)$/.test(code);

  function onkeydown(e: KeyboardEvent): void {
    e.preventDefault();
    e.stopPropagation();
    const k = scanKey(e.code);
    if (!k) return;
    pressedKeys.set(e.code, k);
    send({ t: 'k', sc: k.sc, ext: k.ext, d: true });
  }

  function onkeyup(e: KeyboardEvent): void {
    e.preventDefault();
    e.stopPropagation();
    const k = scanKey(e.code);
    if (!k) return;
    pressedKeys.delete(e.code);
    send({ t: 'k', sc: k.sc, ext: k.ext });
    // a Mac browser sends no key-up for the keys pressed together with Cmd: let go of them with it
    if (e.code === 'MetaLeft' || e.code === 'MetaRight') {
      for (const [code, key] of [...pressedKeys]) {
        if (isModifier(code)) continue;
        pressedKeys.delete(code);
        send({ t: 'k', sc: key.sc, ext: key.ext });
      }
    }
  }

  /** The focus left the screen: whatever is held down on the remote side is let go. */
  function releaseAll(sendIt = true): void {
    if (sendIt) {
      for (const k of pressedKeys.values()) send({ t: 'k', sc: k.sc, ext: k.ext });
      for (const b of pressedButtons) send({ t: 'b', b, d: false, x: last.x, y: last.y });
    }
    pressedKeys.clear();
    pressedButtons.clear();
  }

  // The listeners that must not be passive (the wheel scrolls the page otherwise) and the keys while the screen has the focus.
  $effect(() => {
    const c = canvas;
    if (!c) return;
    const noMenu = (e: Event) => e.preventDefault();
    const blur = () => releaseAll();
    c.addEventListener('wheel', onwheel, { passive: false });
    c.addEventListener('keydown', onkeydown);
    c.addEventListener('keyup', onkeyup);
    c.addEventListener('contextmenu', noMenu);
    c.addEventListener('blur', blur);
    return () => {
      c.removeEventListener('wheel', onwheel);
      c.removeEventListener('keydown', onkeydown);
      c.removeEventListener('keyup', onkeyup);
      c.removeEventListener('contextmenu', noMenu);
      c.removeEventListener('blur', blur);
    };
  });

  // ---- what the toolbar can do ----

  export function focus(): void {
    canvas?.focus();
  }

  export function disconnect(): void {
    shutdown();
    end(t('assist.disconnected'));
  }

  export function reconnect(): void {
    connect();
    focus();
  }

  export function ctrlAltDel(): void {
    send({ t: 'cad' });
    focus();
  }

  /** Picks a monitor of the computer (its index in info.monitors) or -1 for all of them as one picture; the computer answers with a new Hello. */
  export function selectMonitor(index: number): void {
    if (socket?.readyState === WebSocket.OPEN && phase === 'open') socket.send(encodeMonitor(index));
    focus();
  }

  export const clipboardAvailable = (): boolean => typeof navigator !== 'undefined' && !!navigator.clipboard?.readText && !!navigator.clipboard?.writeText;

  /** The local clipboard (text) to the computer; the browser asks for permission. */
  export async function sendClipboard(): Promise<void> {
    if (socket?.readyState !== WebSocket.OPEN || phase !== 'open') return;
    try {
      const msg = encodeClipSet(await navigator.clipboard.readText());
      if (!msg) {
        showToast(t('assist.clipTooBig'));
        return;
      }
      socket?.send(msg);
      showToast(t('assist.clipSent'));
    } catch {
      showToast(t('assist.clipDenied'));
    }
  }

  /** Asks for the clipboard (text) of the computer; it goes into the local one when the answer comes. */
  export function getClipboard(): void {
    if (socket?.readyState !== WebSocket.OPEN || phase !== 'open') return;
    clipWanted = Date.now() + 15000;
    socket.send(encodeClipGet());
  }

  /** Presses the keys one after the other and lets them go in reverse; the codes are KeyboardEvent.code names. */
  export function combo(codes: string[]): void {
    const keys = codes.map((c) => scanKey(c)).filter((k): k is ScanKey => k !== null);
    for (const k of keys) send({ t: 'k', sc: k.sc, ext: k.ext, d: true });
    for (const k of [...keys].reverse()) send({ t: 'k', sc: k.sc, ext: k.ext });
    focus();
  }

  export function winKey(): void {
    combo(['MetaLeft']);
  }

  export function setScale(kind: AssistScale): void {
    mode = kind;
  }

  export function fullscreen(): void {
    void host?.requestFullscreen?.();
  }

  const label = $derived(session === 'console' ? $tr('assist.console') : $tr('assist.sessionN', { n: session }));
</script>

<div class="asst" bind:this={host}>
  <div class="view" class:real={mode === 'real'} bind:clientWidth={box.w} bind:clientHeight={box.h}>
    <canvas
      bind:this={canvas}
      tabindex="0"
      aria-label={$tr('assist.screenLabel', { name: label })}
      style:width="{cssW}px"
      style:height="{cssH}px"
      style:cursor={hideCursor ? 'default' : 'none'}
      {onpointermove}
      {onpointerdown}
      {onpointerup}
      onpointercancel={onpointerup}
    ></canvas>
  </div>

  {#if phase !== 'open'}
    <div class="veil" role={phase === 'closed' ? 'alert' : 'status'}>
      {#if phase === 'connecting'}
        <div class="spin" aria-hidden="true"><i></i><i></i><i></i><i></i></div>
        <p>{$tr('assist.connecting', { name: label })}</p>
        <p class="small hint">{$tr('assist.connectingHint')}</p>
      {:else}
        <h3>{$tr('assist.closedTitle')}</h3>
        <p class="reason">{message}</p>
        <button class="btn primary" type="button" onclick={reconnect}>{$tr('assist.reconnect')}</button>
      {/if}
    </div>
  {/if}
</div>

<style>
  .asst {
    position: relative;
    flex: 1;
    width: 100%;
    height: 100%;
    min-width: 0;
    min-height: 0;
    display: flex;
    background: #0b0e14;
  }

  .view {
    flex: 1;
    min-width: 0;
    min-height: 0;
    display: flex;
    overflow: hidden;
  }

  .view.real {
    overflow: auto;
  }

  canvas {
    flex: none;
    margin: auto;
    background: #000;
    touch-action: none;
    user-select: none;
    -webkit-user-select: none;
    image-rendering: auto;
  }

  canvas:focus {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
  }

  .veil {
    position: absolute;
    inset: 0;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 11px;
    padding: 20px;
    text-align: center;
    color: #e6e9ef;
    background: #0b0e14;
  }

  .veil p,
  .veil h3 {
    margin: 0;
  }

  .veil h3 {
    font-size: 20px;
  }

  .hint {
    color: #8a93a4;
    max-width: 380px;
  }

  .reason {
    max-width: 420px;
    color: #c6ccd8;
    overflow-wrap: anywhere;
  }

  .spin {
    display: flex;
    gap: 6px;
  }

  .spin i {
    width: 10px;
    height: 10px;
    background: var(--accent);
    animation: blink 1s steps(1) infinite;
  }

  .spin i:nth-child(2) {
    animation-delay: 0.2s;
  }

  .spin i:nth-child(3) {
    animation-delay: 0.4s;
  }

  .spin i:nth-child(4) {
    animation-delay: 0.6s;
  }

  @keyframes blink {
    0% {
      opacity: 1;
    }

    50% {
      opacity: 0.15;
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .spin i {
      animation: none;
    }
  }
</style>
