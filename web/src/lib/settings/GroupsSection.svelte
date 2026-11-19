<script lang="ts">
  import PixIcon from '../ui/PixIcon.svelte';
  import { onMount } from 'svelte';
  import { api, ApiError, isUnauthorized, type Group } from '../../api';
  import { t, tr } from '../../i18n';

  let { onauthlost }: { onauthlost: () => void } = $props();

  let groups = $state<Group[]>([]);
  let loaded = $state(false);
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

  async function load(): Promise<void> {
    try {
      groups = await api.groups();
      names = {};
    } catch (e) {
      fail(e);
    } finally {
      loaded = true;
    }
  }

  /** Runs a change, then reloads the list (the order and the names come from the server). */
  async function run(action: () => Promise<unknown>): Promise<boolean> {
    if (busy) return false;
    busy = true;
    error = '';
    try {
      await action();
      await load();
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

  async function move(index: number, dir: -1 | 1): Promise<void> {
    const ids = groups.map((g) => g.id);
    const to = index + dir;
    if (to < 0 || to >= ids.length) return;
    [ids[index], ids[to]] = [ids[to], ids[index]];
    await run(() => api.groupsReorder(ids));
  }

  onMount(() => void load());
</script>

<div class="groups">
  <h3>{$tr('dash.group.title')}</h3>
  <p class="muted">{$tr('dash.group.hint')}</p>

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

  {#if !loaded}
    <p class="muted">{$tr('common.loading')}</p>
  {:else if groups.length === 0}
    <p class="muted small">{$tr('dash.group.empty')}</p>
  {:else}
    <ul class="list">
      {#each groups as group, i (group.id)}
        <li class="item">
          <input
            class="input name"
            maxlength="40"
            aria-label={$tr('dash.group.rename')}
            value={names[group.id] ?? group.name}
            oninput={(event) => (names[group.id] = event.currentTarget.value)}
            onkeydown={(event) => event.key === 'Enter' && void rename(group)} />
          <div class="row acts">
            <button class="btn sq" type="button" title={$tr('dash.group.up')} aria-label={$tr('dash.group.up')} disabled={busy || i === 0} onclick={() => void move(i, -1)}><PixIcon name="arrow-up" size={24} /></button>
            <button class="btn sq" type="button" title={$tr('dash.group.down')} aria-label={$tr('dash.group.down')} disabled={busy || i === groups.length - 1} onclick={() => void move(i, 1)}><PixIcon name="arrow-down" size={24} /></button>
            <button
              class="btn"
              type="button"
              disabled={busy || (names[group.id] ?? group.name).trim() === group.name}
              onclick={() => void rename(group)}>{$tr('dash.group.save')}</button>
            <button class="btn" class:danger={confirming === group.id} type="button" disabled={busy} onclick={() => void remove(group)}>
              {confirming === group.id ? $tr('dash.group.confirm') : $tr('dash.group.delete')}
            </button>
          </div>
        </li>
      {/each}
    </ul>
  {/if}
</div>

<style>
  .groups {
    display: flex;
    flex-direction: column;
    gap: 11px;
    min-width: 0;
  }

  h3,
  p {
    margin: 0;
  }

  .grow {
    flex: 1 1 160px;
    min-width: 0;
  }

  .list {
    display: flex;
    flex-direction: column;
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .item {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 7px 11px;
    padding: 9px 0;
    border-bottom: 1px dashed var(--seg-off);
  }

  .name {
    flex: 1 1 220px;
    min-width: 0;
  }

  .acts {
    flex-wrap: nowrap;
  }

  @media (max-width: 720px) {
    .name {
      flex-basis: 100%;
    }

    .acts {
      width: 100%;
    }

    .sq {
      width: 44px;
    }

    .acts .btn:not(.sq) {
      flex: 1;
    }
  }
</style>
