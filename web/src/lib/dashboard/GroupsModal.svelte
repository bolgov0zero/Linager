<script lang="ts">
  import { api, ApiError, isUnauthorized, type Group } from '../../api';
  import { t, tr } from '../../i18n';
  import Modal from '../Modal.svelte';

  let {
    groups,
    onclose,
    onchanged,
    onauthlost,
  }: {
    groups: Group[];
    onclose: () => void;
    /** Groups were created, renamed or deleted: reload the dashboard. */
    onchanged: () => void;
    onauthlost: () => void;
  } = $props();

  let newName = $state('');
  let names = $state<Record<number, string>>({});
  let confirming = $state<number | null>(null);
  let error = $state('');
  let busy = $state(false);

  function fail(e: unknown): void {
    if (isUnauthorized(e)) {
      onauthlost();
      return;
    }
    if (e instanceof ApiError && e.status === 409) error = t('dash.group.exists');
    else if (e instanceof ApiError && e.status === 400) error = t('dash.group.invalid');
    else error = t('dash.group.failed');
  }

  async function run(action: () => Promise<unknown>): Promise<boolean> {
    if (busy) return false;
    busy = true;
    error = '';
    try {
      await action();
      onchanged();
      return true;
    } catch (e) {
      fail(e);
      return false;
    } finally {
      busy = false;
    }
  }

  async function add(): Promise<void> {
    const name = newName.trim();
    if (name && (await run(() => api.groupCreate(name)))) newName = '';
  }

  async function rename(group: Group): Promise<void> {
    const name = (names[group.id] ?? group.name).trim();
    if (name && name !== group.name) await run(() => api.groupRename(group.id, name));
  }

  async function remove(group: Group): Promise<void> {
    if (confirming !== group.id) {
      confirming = group.id;
      return;
    }
    confirming = null;
    await run(() => api.groupDelete(group.id));
  }
</script>

<Modal title={$tr('dash.group.title')} labelId="groups-title" {onclose}>
  <form
    class="row"
    onsubmit={(event) => {
      event.preventDefault();
      void add();
    }}>
    <input class="input grow" maxlength="40" placeholder={$tr('dash.group.new')} aria-label={$tr('dash.group.new')} bind:value={newName} />
    <button class="btn primary" type="submit" disabled={busy || !newName.trim()}>{$tr('dash.group.add')}</button>
  </form>

  {#if error}
    <p class="error" role="alert">{error}</p>
  {/if}

  {#if groups.length === 0}
    <p class="muted small">{$tr('dash.group.empty')}</p>
  {:else}
    <ul class="list">
      {#each groups as group (group.id)}
        <li class="row">
          <input
            class="input grow"
            maxlength="40"
            aria-label={$tr('dash.group.rename')}
            value={names[group.id] ?? group.name}
            oninput={(event) => (names[group.id] = event.currentTarget.value)}
            onkeydown={(event) => event.key === 'Enter' && void rename(group)} />
          <button
            class="btn"
            type="button"
            disabled={busy || (names[group.id] ?? group.name).trim() === group.name}
            onclick={() => void rename(group)}>{$tr('dash.group.save')}</button>
          <button class="btn" class:danger={confirming === group.id} type="button" disabled={busy} onclick={() => void remove(group)}>
            {confirming === group.id ? $tr('dash.group.confirm') : $tr('dash.group.delete')}
          </button>
        </li>
      {/each}
    </ul>
  {/if}
</Modal>

<style>
  .grow {
    flex: 1 1 160px;
    min-width: 0;
  }

  .list {
    margin: 0;
    padding: 0;
    list-style: none;
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  .error {
    margin: 0;
  }
</style>
