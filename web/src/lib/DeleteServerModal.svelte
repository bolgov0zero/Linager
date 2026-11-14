<script lang="ts">
  import { api, isUnauthorized } from '../api';
  import { t, tr } from '../i18n';
  import Modal from './Modal.svelte';

  let {
    server,
    onclose,
    ondeleted,
    onauthlost,
  }: {
    /** A server from the server page or a card on the dashboard. */
    server: { id: string; name: string; online: boolean };
    onclose: () => void;
    ondeleted: () => void;
    onauthlost: () => void;
  } = $props();

  // Uninstall is checked by default. Offline servers keep it checked but locked.
  let uninstall = $state(true);
  let typedName = $state('');
  let busy = $state(false);
  let error = $state<string | null>(null);

  const canDelete = $derived(!busy && typedName.trim() === server.name.trim());

  async function confirmDelete(): Promise<void> {
    if (!canDelete) return;
    busy = true;
    error = null;
    try {
      await api.deleteServer(server.id, server.name, uninstall);
      ondeleted();
    } catch (e) {
      if (isUnauthorized(e)) {
        onauthlost();
        return;
      }
      error = t('delete.failed');
    } finally {
      busy = false;
    }
  }
</script>

<Modal title={$tr('delete.title')} labelId="delete-title" {onclose}>
  <form
    class="stack"
    onsubmit={(event) => {
      event.preventDefault();
      void confirmDelete();
    }}
  >
    <p>{$tr('delete.text')}</p>

    <div class="stack">
      <label class="check">
        <input type="checkbox" bind:checked={uninstall} disabled={!server.online} />
        <span>{$tr('delete.uninstall')}</span>
      </label>
      {#if !server.online}
        <p class="hint">{$tr('delete.uninstallOffline')}</p>
      {/if}
    </div>

    <label class="field">
      <span class="label">{$tr('delete.typeName', { name: server.name })}</span>
      <input class="input" type="text" autocomplete="off" bind:value={typedName} />
    </label>

    {#if error}
      <p class="error" role="alert">{error}</p>
    {/if}

    <div class="row end">
      <button class="btn" type="button" disabled={busy} onclick={onclose}>{$tr('common.cancel')}</button>
      <button class="btn danger" type="submit" disabled={!canDelete}>
        {busy ? $tr('delete.deleting') : $tr('delete.confirm')}
      </button>
    </div>
  </form>
</Modal>
