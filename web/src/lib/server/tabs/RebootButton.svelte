<script lang="ts">
  import { api, type ServerDetail } from '../../../api';
  import { tr } from '../../../i18n';
  import Modal from '../../Modal.svelte';
  import { showToast } from '../../toast';
  import { errorText } from './svc';

  let { server }: { server: ServerDetail } = $props();

  let open = $state(false);
  let typed = $state('');
  let busy = $state(false);

  const canConfirm = $derived(typed.trim() === server.name && !busy);

  function openModal() {
    typed = '';
    open = true;
  }

  function close() {
    if (!busy) open = false;
  }

  async function confirmReboot() {
    if (!canConfirm) return;
    busy = true;
    try {
      await api.reboot(server.id);
      open = false;
      showToast($tr('reboot.started'));
    } catch (e) {
      showToast(errorText(e, $tr));
    } finally {
      busy = false;
    }
  }
</script>

<button class="btn" type="button" disabled={!server.online} onclick={openModal}>{$tr('reboot.button')}</button>

{#if open}
  <Modal title={$tr('reboot.title')} labelId="reboot-dialog-title" onclose={close}>
    {#snippet children()}
      <p class="text">{$tr('reboot.text')}</p>
      <p class="mono">{$tr('reboot.server', { name: server.name })}</p>
      <div class="field">
        <label class="label" for="reboot-typed">{$tr('reboot.typeName', { name: server.name })}</label>
        <input
          id="reboot-typed"
          class="input"
          type="text"
          autocomplete="off"
          spellcheck="false"
          bind:value={typed}
          disabled={busy}
        />
      </div>
      <div class="row end">
        <button class="btn" type="button" disabled={busy} onclick={close}>{$tr('common.cancel')}</button>
        <button class="btn danger" type="button" disabled={!canConfirm} onclick={confirmReboot}>
          {$tr('reboot.confirm')}
        </button>
      </div>
    {/snippet}
  </Modal>
{/if}

<style>
  .text {
    margin: 0;
  }

  .label {
    color: var(--muted);
  }
</style>
