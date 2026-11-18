<script lang="ts">
  import { tr } from '../../../i18n';
  import Modal from '../../Modal.svelte';

  /** A question before a dangerous action. */
  let { text, danger = false, onyes, onclose }: { text: string; danger?: boolean; onyes: () => void; onclose: () => void } = $props();
</script>

<Modal title={$tr('win.confirm')} labelId="win-confirm-title" {onclose}>
  <p>{text}</p>
  <div class="row">
    <button class="btn" type="button" onclick={onclose}>{$tr('win.cancel')}</button>
    <button class="btn" class:danger type="button" onclick={onyes}>{$tr('win.confirm')}</button>
  </div>
</Modal>
<style>
  .row {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    margin-top: 12px;
  }

</style>
