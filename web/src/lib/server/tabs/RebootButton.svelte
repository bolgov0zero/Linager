<script lang="ts">
  import { api, type ServerDetail } from '../../../api';
  import { tr } from '../../../i18n';
  import Modal from '../../Modal.svelte';
  import PixIcon from '../../ui/PixIcon.svelte';
  import { showToast } from '../../toast';
  import { errorText } from './svc';

  /** icon: only the power icon (for the top bar); the label becomes the tooltip. */
  let { server, icon = false }: { server: ServerDetail; icon?: boolean } = $props();

  // The button may sit in the pinned top bar; the dialog must not live inside its stacking context.
  function portal(node: HTMLElement) {
    document.body.appendChild(node);
    return { destroy: () => node.remove() };
  }

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

{#if icon}
  <button class="btn tb-ic" type="button" title={$tr('reboot.button')} aria-label={$tr('reboot.button')} disabled={!server.online} onclick={openModal}><PixIcon name="reboot" size={24} /></button>
{:else}
  <button class="btn" type="button" disabled={!server.online} onclick={openModal}>{$tr('reboot.button')}</button>
{/if}

{#if open}
  <div use:portal>
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
  </div>
{/if}

<style>
  .text {
    margin: 0;
  }

  .label {
    color: var(--muted);
  }
</style>
