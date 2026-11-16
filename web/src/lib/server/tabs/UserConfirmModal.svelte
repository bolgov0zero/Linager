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
