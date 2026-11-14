<script lang="ts">
  import { tr } from '../../i18n';
  import TermSession, { type TermPhase } from './TermSession.svelte';
  import { closeTermWindow, focusTermWindow, updateTermWindow, type TermWindow } from './windows';

  let { win, top }: { win: TermWindow; top: boolean } = $props();

  const MIN_W = 360;
  const MIN_H = 220;

  let phase = $state<TermPhase>('connecting');
  let session = $state<TermSession | null>(null);

  // The key and server never change for a window; deriving them keeps the session from restarting
  // when the store replaces the window object on every move.
  const serverId = $derived(win.serverId);
  const key = $derived(win.key);

  const statusText = $derived(
    phase === 'connecting' ? $tr('term.connecting') : phase === 'open' ? $tr('term.open') : $tr('term.closed'),
  );

  const narrow = () => window.matchMedia('(max-width: 600px)').matches;

  type Edge = 'move' | 'e' | 's' | 'se';

  /** Drag the window (header) or one of its resize handles with pointer capture. */
  function startDrag(event: PointerEvent, edge: Edge): void {
    if (event.button !== 0 || win.maximized || narrow()) return;
    if (edge === 'move' && (event.target as HTMLElement).closest('button')) return;
    event.preventDefault();
    const handle = event.currentTarget as HTMLElement;
    handle.setPointerCapture(event.pointerId);
    const start = { px: event.clientX, py: event.clientY, x: win.x, y: win.y, w: win.w, h: win.h };
    const vw = window.innerWidth;
    const vh = window.innerHeight;

    const move = (e: PointerEvent) => {
      const dx = e.clientX - start.px;
      const dy = e.clientY - start.py;
      if (edge === 'move') {
        // Keep the header reachable: at least 80px of it stays on screen, its top never leaves it.
        updateTermWindow(key, {
          x: Math.min(Math.max(start.x + dx, 80 - start.w), vw - 80),
          y: Math.min(Math.max(start.y + dy, 0), vh - 40),
        });
        return;
      }
      const patch: Partial<TermWindow> = {};
      if (edge !== 's') patch.w = Math.min(Math.max(start.w + dx, MIN_W), vw - start.x);
      if (edge !== 'e') patch.h = Math.min(Math.max(start.h + dy, MIN_H), vh - start.y);
      updateTermWindow(key, patch);
    };
    const end = () => {
      handle.removeEventListener('pointermove', move);
      handle.removeEventListener('pointerup', end);
      handle.removeEventListener('pointercancel', end);
      session?.focus();
    };
    handle.addEventListener('pointermove', move);
    handle.addEventListener('pointerup', end);
    handle.addEventListener('pointercancel', end);
  }

  function toggleMax(): void {
    updateTermWindow(key, { maximized: !win.maximized });
    queueMicrotask(() => session?.focus());
  }
</script>

<section
  class="win"
  class:top
  class:max={win.maximized}
  class:hidden={win.minimized}
  style:left="{win.x}px"
  style:top="{win.y}px"
  style:width="{win.w}px"
  style:height="{win.h}px"
  style:z-index={win.z}
  aria-label={$tr('termwin.title', { name: win.name })}
  aria-hidden={win.minimized}
  onpointerdowncapture={() => focusTermWindow(key)}
>
  <header class="bar" role="presentation" onpointerdown={(e) => startDrag(e, 'move')} ondblclick={toggleMax}>
    <p class="name">
      <span class="dot" class:on={phase === 'open'} aria-hidden="true"></span>
      <span class="label">{win.name}</span>
      <span class="muted status">· {statusText}</span>
    </p>
    <div class="tools">
      {#if phase === 'open'}
        <button class="btn sm" type="button" title={$tr('term.clipboardHint')} onclick={() => session?.copy()}>{$tr('term.copy')}</button>
        <button class="btn sm" type="button" title={$tr('term.clipboardHint')} onclick={() => session?.paste()}>{$tr('term.paste')}</button>
      {/if}
      <button class="btn sm icon" type="button" title={$tr('termwin.minimize')} aria-label={$tr('termwin.minimize')}
        onclick={() => updateTermWindow(key, { minimized: true })}>_</button>
      <button class="btn sm icon wide" type="button"
        title={win.maximized ? $tr('termwin.restore') : $tr('termwin.maximize')}
        aria-label={win.maximized ? $tr('termwin.restore') : $tr('termwin.maximize')}
        onclick={toggleMax}>{win.maximized ? '▭' : '□'}</button>
      <button class="btn sm icon" type="button" title={$tr('termwin.close')} aria-label={$tr('termwin.close')}
        onclick={() => closeTermWindow(key)}>×</button>
    </div>
  </header>
  <div class="body">
    <TermSession {serverId} bind:phase bind:this={session} onlimit={() => closeTermWindow(key)} />
  </div>
  {#if !win.maximized}
    <div class="grip e" role="presentation" onpointerdown={(e) => startDrag(e, 'e')}></div>
    <div class="grip s" role="presentation" onpointerdown={(e) => startDrag(e, 's')}></div>
    <div class="grip se" role="presentation" onpointerdown={(e) => startDrag(e, 'se')}></div>
  {/if}
</section>

<style>
  .win {
    position: absolute;
    display: flex;
    flex-direction: column;
    min-width: 0;
    pointer-events: auto;
    background: var(--surface);
    border: 2px solid var(--line);
    box-shadow: 4px 4px 0 var(--shade);
  }

  .win.max {
    left: 8px !important;
    top: 8px !important;
    width: calc(100% - 16px) !important;
    height: calc(100% - 56px) !important;
  }

  /* Minimized windows keep their size (no PTY resize) and their session; they are only hidden. */
  .win.hidden {
    visibility: hidden;
    pointer-events: none;
  }

  .bar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    padding: 6px 8px 6px 12px;
    border-bottom: 2px solid var(--line);
    cursor: move;
    user-select: none;
    touch-action: none;
  }

  .win.top .bar {
    background: var(--seg-off);
  }

  .win.max .bar {
    cursor: default;
  }

  .name {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
    margin: 0;
    font-size: 12px;
    white-space: nowrap;
  }

  .label {
    font-family: var(--font-display);
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .status {
    overflow: hidden;
    text-overflow: ellipsis;
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

  .tools {
    display: flex;
    flex: none;
    gap: 4px;
  }

  .btn.sm {
    padding: 2px 8px;
    font-size: 12px;
    line-height: 1.4;
  }

  .btn.icon {
    min-width: 28px;
    padding: 2px 4px;
  }

  .body {
    flex: 1;
    min-height: 0;
    padding: 8px;
  }

  .grip {
    position: absolute;
    touch-action: none;
  }

  .grip.e {
    top: 0;
    right: -5px;
    width: 8px;
    height: 100%;
    cursor: ew-resize;
  }

  .grip.s {
    left: 0;
    bottom: -5px;
    width: 100%;
    height: 8px;
    cursor: ns-resize;
  }

  .grip.se {
    right: -5px;
    bottom: -5px;
    width: 14px;
    height: 14px;
    cursor: nwse-resize;
  }

  /* Phones: every window is full screen, no dragging or resizing. */
  @media (max-width: 600px) {
    .win {
      left: 0 !important;
      top: 0 !important;
      width: 100% !important;
      height: calc(100% - 48px) !important;
    }

    .bar {
      cursor: default;
    }

    .grip,
    .btn.wide {
      display: none;
    }

    .status {
      display: none;
    }
  }
</style>
