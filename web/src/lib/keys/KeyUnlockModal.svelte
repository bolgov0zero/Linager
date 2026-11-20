<script lang="ts">
  import { tr } from '../../i18n';
  import Modal from '../Modal.svelte';
  import KeyOpenForm from './KeyOpenForm.svelte';

  /** A small window that opens the vault from a form (the RDP form): the file and the passphrase, nothing else. */
  let { onopened, onclose }: { onopened: () => void; onclose: () => void } = $props();
</script>

<Modal title={$tr('keys.unlock.title')} labelId="keys-unlock-title" {onclose}>
  <p class="muted small">{$tr('keys.unlock.hint')}</p>
  <KeyOpenForm {onopened} />
</Modal>

<style>
  p {
    margin: 0;
  }
</style>
