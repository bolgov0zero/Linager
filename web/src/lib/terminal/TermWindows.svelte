<script lang="ts">
  import { tr } from '../../i18n';
  import EditorWindowView from './EditorWindowView.svelte';
  import TermWindowView from './TermWindowView.svelte';
  import { focusTermWindow, termWindows, updateTermWindow } from './windows';

  /** The administrator, or an operator who was given the Users module, may pick the account of a shell. */
  let { canUsers = false }: { canUsers?: boolean } = $props();

  const topKey = $derived(
    $termWindows.filter((w) => !w.minimized).reduce<{ key: number; z: number } | null>(
      (best, w) => (!best || w.z > best.z ? { key: w.key, z: w.z } : best),
      null,
    )?.key ?? null,
  );

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
  <div class="layer">
    {#each $termWindows as win (win.key)}
      {#if win.kind === 'editor'}
        <EditorWindowView {win} top={win.key === topKey} />
      {:else}
        <TermWindowView {win} top={win.key === topKey} {canUsers} />
      {/if}
    {/each}
    <nav class="dock" aria-label={$tr('termwin.dock')}>
      {#each $termWindows as win (win.key)}
        <button
          class="btn sm"
          class:ink={win.key === topKey}
          class:off={win.minimized}
          type="button"
          title={win.minimized ? $tr('termwin.restore') : $tr('termwin.minimize')}
          onclick={() => toggle(win.key, win.minimized)}>{win.kind === 'editor' ? '✎' : '>_'} {win.name}</button>
      {/each}
    </nav>
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
    left: 16px;
    bottom: 12px;
    z-index: 100000;
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    max-width: calc(100% - 32px);
    pointer-events: auto;
  }

  .btn.sm {
    max-width: 220px;
    overflow: hidden;
    padding: 2px 8px;
    font-size: 12px;
    text-overflow: ellipsis;
    white-space: nowrap;
    box-shadow: 3px 3px 0 var(--shade);
  }

  .btn.off {
    color: var(--muted);
  }
</style>
