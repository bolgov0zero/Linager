<script lang="ts">
  import type { Snippet } from 'svelte';
  import { tr } from '../i18n';

  let {
    title,
    labelId,
    onclose,
    wide = false,
    medium = false,
    flush = false,
    children,
  }: {
    title: string;
    labelId: string;
    onclose: () => void;
    /** A large dialog (settings). */
    wide?: boolean;
    /** A dialog with two columns of fields. */
    medium?: boolean;
    /** No padding around the content (lists of actions on a phone). */
    flush?: boolean;
    children: Snippet;
  } = $props();

  let dialog = $state<HTMLDivElement | undefined>(undefined);

  $effect(() => {
    dialog?.focus();
  });
</script>

<svelte:window
  onkeydown={(event) => {
    if (event.key === 'Escape') onclose();
  }}
/>

<div class="modal-layer">
  <button class="backdrop" type="button" tabindex="-1" aria-label={$tr('common.close')} onclick={onclose}
  ></button>
  <div class="dialog" class:wide class:medium role="dialog" aria-modal="true" aria-labelledby={labelId} tabindex="-1" bind:this={dialog}>
    <header class="dialog-head">
      <h2 id={labelId}>{title}</h2>
      <button class="btn" type="button" aria-label={$tr('common.close')} onclick={onclose}>×</button>
    </header>
    <div class="dialog-body" class:flush>
      {@render children()}
    </div>
  </div>
</div>

<style>
  .modal-layer {
    position: fixed;
    inset: 0;
    z-index: 20;
    display: flex;
    align-items: flex-start;
    justify-content: center;
    padding: 48px 16px 16px;
    overflow: auto;
  }

  .backdrop {
    position: absolute;
    inset: 0;
    border: 0;
    padding: 0;
    cursor: default;
    background: color-mix(in srgb, var(--ink) 55%, transparent);
  }

  .dialog {
    position: relative;
    width: 100%;
    max-width: 560px;
    background: var(--surface);
    border: 2px solid var(--line);
    box-shadow: 4px 4px 0 var(--shade);
    outline: none;
  }

  .dialog.medium {
    max-width: 920px;
  }

  .dialog.wide {
    max-width: min(1360px, 100%);
  }

  .dialog-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 12px 16px;
    border-bottom: 2px solid var(--line);
  }

  .dialog-head h2 {
    margin: 0;
    font-size: 20px;
    line-height: 1.1;
  }

  .dialog-body {
    padding: 16px;
    display: flex;
    flex-direction: column;
    gap: 16px;
  }

  /* Phones: every dialog is a sheet from the bottom edge (a large one fills the screen); only its body scrolls. */
  @media (max-width: 720px) {
    .modal-layer {
      align-items: flex-end;
      padding: 0;
      overflow: hidden;
    }

    .dialog,
    .dialog.medium,
    .dialog.wide {
      display: flex;
      flex-direction: column;
      max-width: none;
      max-height: 92dvh;
      border-width: 2px 0 0;
      box-shadow: 0 -4px 0 var(--shade);
    }

    .dialog.wide {
      height: calc(100dvh - 12px);
      max-height: none;
    }

    .dialog-head {
      flex: none;
      min-height: 52px;
      padding: 4px 6px 4px 14px;
    }

    .dialog-head .btn {
      width: 44px;
      padding: 0;
    }

    .dialog-body {
      flex: 1 1 auto;
      min-height: 0;
      gap: 12px;
      overflow-y: auto;
      overscroll-behavior: contain;
      padding: 12px 12px calc(12px + var(--safe-b));
    }

    .dialog-body.flush {
      padding: 0 0 var(--safe-b);
      gap: 0;
    }

    /* The buttons at the end of a dialog stay in reach while its content scrolls. */
    .dialog-body > :global(.row.end:last-child),
    .dialog-body > :global(form > .row.end:last-child) {
      position: sticky;
      bottom: 0;
      z-index: 2;
      flex-wrap: nowrap;
      margin: 0 -12px calc(-12px - var(--safe-b));
      padding: 10px 12px calc(10px + var(--safe-b));
      background: var(--surface);
      border-top: 2px solid var(--line);
    }

    .dialog-body > :global(.row.end:last-child > .btn),
    .dialog-body > :global(form > .row.end:last-child > .btn) {
      flex: 1;
    }
  }
</style>
