<script lang="ts">
  import type { Snippet } from 'svelte';
  import { tr } from '../../i18n';
  import { viewBox } from './vv';
  import { closeTermWindow, focusTermWindow, updateTermWindow, type TermWindow } from './windows';

  /**
   * A floating window: header (drag, double click maximizes), minimize / maximize / close, resize handles.
   * The content comes from snippets; the owner decides what closing does.
   */
  let {
    win,
    top,
    ariaLabel,
    onclose,
    onrefocus,
    title,
    tools,
    children,
  }: {
    win: TermWindow;
    top: boolean;
    ariaLabel: string;
    /** Close button; defaults to closing the window. */
    onclose?: () => void;
    /** Called after a drag or maximize, e.g. to give the content focus back. */
    onrefocus?: () => void;
    title: Snippet;
    tools?: Snippet;
    children: Snippet;
  } = $props();

  const MIN_W = 360;
  const MIN_H = 220;

  const key = $derived(win.key);
  const narrow = () => window.matchMedia('(max-width: 720px)').matches;

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
      onrefocus?.();
    };
    handle.addEventListener('pointermove', move);
    handle.addEventListener('pointerup', end);
    handle.addEventListener('pointercancel', end);
  }

  function toggleMax(): void {
    updateTermWindow(key, { maximized: !win.maximized });
    queueMicrotask(() => onrefocus?.());
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
  style:--vv-top={$viewBox.kbd ? `${$viewBox.top}px` : undefined}
  style:--vv-h={$viewBox.kbd ? `${$viewBox.height}px` : undefined}
  aria-label={ariaLabel}
  aria-hidden={win.minimized}
  onpointerdowncapture={() => focusTermWindow(key)}
>
  <header class="bar" role="presentation" onpointerdown={(e) => startDrag(e, 'move')} ondblclick={toggleMax}>
    <p class="name">{@render title()}</p>
    <div class="tools">
      {@render tools?.()}
      <button class="btn sm icon wbtn" type="button" title={$tr('termwin.minimize')} aria-label={$tr('termwin.minimize')}
        onclick={() => updateTermWindow(key, { minimized: true })}>_</button>
      <button class="btn sm icon wide" type="button"
        title={win.maximized ? $tr('termwin.restore') : $tr('termwin.maximize')}
        aria-label={win.maximized ? $tr('termwin.restore') : $tr('termwin.maximize')}
        onclick={toggleMax}>{win.maximized ? '▭' : '□'}</button>
      <button class="btn sm icon wbtn" type="button" title={$tr('termwin.close')} aria-label={$tr('termwin.close')}
        onclick={() => (onclose ? onclose() : closeTermWindow(key))}>×</button>
    </div>
    <span class="brk" aria-hidden="true"></span>
  </header>
  <div class="body">
    {@render children()}
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
    left: 7px !important;
    top: 7px !important;
    width: calc(100% - 14px) !important;
    height: calc(100% - 50px) !important;
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
    gap: 7px;
    padding: 5px 7px 5px 11px;
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
    gap: 7px;
    min-width: 0;
    margin: 0;
    font-size: 11px;
    white-space: nowrap;
  }

  .name :global(.label) {
    font-family: var(--font-display);
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .name :global(.status) {
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .name :global(.dot) {
    flex: none;
    width: 9px;
    height: 9px;
    border: 2px solid var(--line);
    background: var(--muted);
  }

  .name :global(.dot.on) {
    background: var(--good);
  }

  .tools {
    display: flex;
    flex: none;
    gap: 4px;
  }

  .tools :global(.btn.sm) {
    padding: 2px 7px;
    font-size: 11px;
    line-height: 1.4;
  }

  /* a pixel icon (18 px) is taller than the 11 px text of a small button: it must not make the button taller than its neighbours */
  .tools :global(.btn.sm .pix) {
    margin: -2px 0;
  }

  .tools :global(.btn.icon) {
    min-width: 25px;
    padding: 2px 4px;
  }

  .body {
    flex: 1;
    min-height: 0;
    padding: 7px;
  }

  .grip {
    position: absolute;
    touch-action: none;
  }

  .grip.e {
    top: 0;
    right: -5px;
    width: 7px;
    height: 100%;
    cursor: ew-resize;
  }

  .grip.s {
    left: 0;
    bottom: -5px;
    width: 100%;
    height: 7px;
    cursor: ns-resize;
  }

  .grip.se {
    right: -5px;
    bottom: -5px;
    width: 13px;
    height: 13px;
    cursor: nwse-resize;
  }

  .brk {
    display: none;
  }

  /* Phones: every window is full screen above the page (no dragging or resizing), with the dock strip under it; while
     the keyboard is up the window is exactly the visible part of the screen. The header has two rows: the name with
     minimize and close, then the tools. */
  @media (max-width: 720px) {
    .win,
    .win.max {
      left: 0 !important;
      top: var(--vv-top, 0px) !important;
      width: 100% !important;
      height: var(--vv-h, calc(100% - var(--dock-h, 52px))) !important;
      border-width: 0;
      box-shadow: none;
    }

    .bar {
      flex-wrap: wrap;
      justify-content: flex-start;
      gap: 0 5px;
      padding: calc(2px + var(--safe-t)) 7px 5px 11px;
      cursor: default;
    }

    .name {
      flex: 1 1 0;
      min-height: 44px;
    }

    .tools {
      display: contents;
    }

    .tools > :global(*) {
      order: 2;
    }

    .tools > :global(.wbtn) {
      order: 1;
    }

    .brk {
      display: block;
      order: 1;
      flex: 0 0 100%;
      height: 0;
    }

    .tools :global(.btn.sm),
    .tools :global(.btn.icon) {
      min-width: 44px;
      font-size: 12px;
    }

    .tools :global(.btn.sm) {
      box-shadow: none;
    }

    .grip,
    .tools :global(.btn.wide) {
      display: none;
    }

    .name :global(.status) {
      display: none;
    }

    .body {
      padding: 0;
      overflow: hidden;
    }
  }
</style>
