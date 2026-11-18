<script lang="ts">
  import { tr } from '../../../i18n';
  import Modal from '../../Modal.svelte';

  // A plain confirmation for an action on an account (lock, unlock, end the sessions).
  let {
    title,
    text,
    hint = '',
    confirmLabel,
    danger = false,
    onclose,
    onconfirm,
  }: {
    title: string;
    text: string;
    hint?: string;
    confirmLabel: string;
    danger?: boolean;
    onclose: () => void;
    /** Resolves with an error text to keep the dialog open, or null when it worked. */
    onconfirm: () => Promise<string | null>;
  } = $props();

  let busy = $state(false);
  let error = $state<string | null>(null);

  async function go(): Promise<void> {
    if (busy) return;
    busy = true;
    error = null;
    error = await onconfirm();
    busy = false;
    if (!error) onclose();
  }
</script>

<Modal {title} labelId="usr-confirm" {onclose}>
  <div class="stack">
    <p>{text}</p>
    {#if hint}<p class="muted small">{hint}</p>{/if}
    {#if error}<p class="error" role="alert">{error}</p>{/if}
    <div class="row end">
      <button class="btn" type="button" onclick={onclose}>{$tr('common.cancel')}</button>
      <button class="btn" class:danger class:primary={!danger} type="button" disabled={busy} onclick={() => void go()}>{confirmLabel}</button>
    </div>
  </div>
</Modal>

<style>
  @media (max-width: 720px) {
    .stack > :global(.row.end:last-child) {
      position: sticky;
      bottom: 0;
      z-index: 2;
      flex-wrap: nowrap;
      margin: 0 -11px calc(-11px - var(--safe-b));
      padding: 9px 11px calc(9px + var(--safe-b));
      background: var(--surface);
      border-top: 2px solid var(--line);
    }

    .stack > :global(.row.end:last-child > .btn) {
      flex: 1;
    }
  }
</style>
