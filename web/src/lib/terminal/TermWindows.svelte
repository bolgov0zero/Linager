<script lang="ts">
  import { tr } from '../../i18n';
  import AssistantWindowView from '../assistant/AssistantWindowView.svelte';
  import DesktopWindowView from '../desktop/DesktopWindowView.svelte';
  import { pultShown } from '../pult/store';
  import EditorWindowView from './EditorWindowView.svelte';
  import FilesWindowView from './FilesWindowView.svelte';
  import QualityWindowView from '../assistant/QualityWindowView.svelte';
  import TermWindowView from './TermWindowView.svelte';
  import { lockScroll } from '../ui/scrolllock';
  import { isPhone } from '../ui/viewport';
  import { viewBox } from './vv';
  import { focusTermWindow, termWindows, updateTermWindow } from './windows';

  /** The administrator, or an operator who was given the Users module, may pick the account of a shell. */
  let { canUsers = false }: { canUsers?: boolean } = $props();

  const topKey = $derived(
    $termWindows.filter((w) => !w.minimized).reduce<{ key: number; z: number } | null>(
      (best, w) => (!best || w.z > best.z ? { key: w.key, z: w.z } : best),
      null,
    )?.key ?? null,
  );

  // Phone: a window covers the whole screen, the page behind it must not scroll when a finger moves over the window.
  $effect(() => {
    if (!$isPhone || topKey === null) return;
    return lockScroll();
  });

  // Phone: while every window is minimized the dock sits above the bottom bar; the page (and its pinned bars) make room.
  $effect(() => {
    if (!$isPhone || $termWindows.length === 0 || topKey !== null || $viewBox.kbd) return;
    const root = document.documentElement;
    root.style.setProperty('--bar-h', 'calc(var(--tab-h) + 54px)');
    return () => root.style.removeProperty('--bar-h');
  });

  /** Taskbar click: restore a minimized window, minimize the front one, bring any other to the front. */
  function toggle(key: number, minimized: boolean): void {
    if (!minimized && key === topKey) updateTermWindow(key, { minimized: true });
    else focusTermWindow(key);
  }

  /** Keeps windows reachable after the browser window shrinks. */
  function clampAll(): void {
    const vw = window.innerWidth;
    const vh = window.innerHeight;
    for (const w of $termWindows) {
      const x = Math.min(w.x, Math.max(0, vw - 80));
      const y = Math.min(w.y, Math.max(0, vh - 40));
      if (x !== w.x || y !== w.y) updateTermWindow(w.key, { x, y });
    }
  }
</script>

<svelte:window onresize={clampAll} />

{#if $termWindows.length > 0}
  <div class="layer" class:idle={topKey === null} class:kbd={$viewBox.kbd}>
    {#each $termWindows as win (win.key)}
      {#if win.kind === 'editor'}
        <EditorWindowView {win} top={win.key === topKey} />
      {:else if win.kind === 'files'}
        <FilesWindowView {win} top={win.key === topKey} />
      {:else if win.kind === 'quality'}
        <QualityWindowView {win} top={win.key === topKey} />
      {:else if win.kind === 'desktop'}
        <DesktopWindowView {win} top={win.key === topKey} />
      {:else if win.kind === 'assistant'}
        <AssistantWindowView {win} top={win.key === topKey} />
      {:else}
        <TermWindowView {win} top={win.key === topKey} {canUsers} />
      {/if}
    {/each}
    {#if !$pultShown || $isPhone}
    <nav class="dock" aria-label={$tr('termwin.dock')}>
      {#each $termWindows as win (win.key)}
        <button
          class="btn sm"
          class:ink={win.key === topKey}
          class:off={win.minimized}
          type="button"
          title={win.minimized ? $tr('termwin.restore') : $tr('termwin.minimize')}
          onclick={() => toggle(win.key, win.minimized)}>{win.kind === 'editor' ? '✎' : win.kind === 'files' ? '▤' : win.kind === 'assistant' ? '▣' : win.kind === 'quality' ? '⚙' : '>_'} {win.name}</button>
      {/each}
    </nav>
    {/if}
  </div>
{/if}

<style>
  /* Above the page, below modal dialogs (z 20) and the toast (z 30); the dashboard stays usable. */
  .layer {
    position: fixed;
    inset: 0;
    z-index: 15;
    pointer-events: none;
    isolation: isolate;
  }

  .dock {
    position: absolute;
    left: 14px;
    bottom: 11px;
    z-index: 100000;
    display: flex;
    flex-wrap: wrap;
    gap: 5px;
    max-width: calc(100% - 29px);
    pointer-events: auto;
  }

  .btn.sm {
    max-width: 198px;
    overflow: hidden;
    padding: 2px 7px;
    font-size: 11px;
    text-overflow: ellipsis;
    white-space: nowrap;
    box-shadow: 3px 3px 0 var(--shade);
  }

  .btn.off {
    color: var(--muted);
  }

  /* Phones: the dock is a strip along the bottom edge, under the full-screen windows (above the bar while every
     window is minimized, so the bar stays reachable) and hidden while the keyboard is up. */
  .layer {
    --dock-h: calc(52px + var(--safe-b));
  }

  @media (max-width: 720px) {
    .dock {
      left: 0;
      right: 0;
      bottom: 0;
      flex-wrap: nowrap;
      gap: 4px;
      max-width: none;
      overflow-x: auto;
      padding: 4px 13px calc(4px + var(--safe-b));
      background: var(--surface);
      border-top: 2px solid var(--line);
    }

    .layer.idle .dock {
      bottom: calc(var(--tab-h) + var(--safe-b));
      padding-bottom: 4px;
    }

    .layer.kbd .dock {
      display: none;
    }

    /* While the keyboard is up a window only covers the visible area; the strip the keyboard and its accessory bar
       (translucent on newer iOS) leave over the page must not show the page. */
    .layer.kbd:not(.idle)::before {
      content: '';
      position: absolute;
      inset: 0;
      z-index: -1;
      background: var(--surface);
      pointer-events: auto;
    }

    .btn.sm {
      flex: 1 1 0;
      max-width: none;
      min-width: 96px;
      padding: 0 7px;
      font-size: 12px;
      box-shadow: none;
    }
  }
</style>
