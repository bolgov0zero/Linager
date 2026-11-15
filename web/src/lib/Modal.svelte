<script lang="ts">
  import type { Snippet } from 'svelte';
  import { tr } from '../i18n';

  let {
    title,
    labelId,
    onclose,
    wide = false,
    children,
  }: {
    title: string;
    labelId: string;
    onclose: () => void;
    /** A large dialog (settings). */
    wide?: boolean;
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
  <div class="dialog" class:wide role="dialog" aria-modal="true" aria-labelledby={labelId} tabindex="-1" bind:this={dialog}>
    <header class="dialog-head">
      <h2 id={labelId}>{title}</h2>
      <button class="btn" type="button" aria-label={$tr('common.close')} onclick={onclose}>×</button>
    </header>
    <div class="dialog-body">
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

  .dialog.wide {
    max-width: min(1120px, 100%);
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
</style>
