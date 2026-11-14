<script lang="ts">
  import { onMount } from 'svelte';
  import { api, ApiError, isUnauthorized, type AvailableServer } from '../../api';
  import { t, tr } from '../../i18n';
  import Modal from '../Modal.svelte';
  import { showToast } from '../toast';

  let {
    onclose,
    onadded,
    onpairing,
    onauthlost,
  }: {
    onclose: () => void;
    /** A server was put on the dashboard. */
    onadded: () => void;
    /** Open the pairing dialog (pending enrollments). */
    onpairing: () => void;
    onauthlost: () => void;
  } = $props();

  const REFRESH_MS = 5000;

  let available = $state<AvailableServer[]>([]);
  let pendingCount = $state(0);
  let loaded = $state(false);
  let loadFailed = $state(false);
  let busy = $state<Record<string, boolean>>({});

  async function load(): Promise<void> {
    try {
      const [servers, enrollments] = await Promise.all([api.availableServers(), api.enrollments()]);
      available = servers;
      pendingCount = enrollments.length;
      loadFailed = false;
    } catch (e) {
      if (isUnauthorized(e)) {
        onauthlost();
        return;
      }
      loadFailed = true;
    } finally {
      loaded = true;
    }
  }

  async function add(server: AvailableServer): Promise<void> {
    if (busy[server.id]) return;
    busy[server.id] = true;
    try {
      await api.dashboardAdd(server.id);
      showToast(t('dash.added'));
      onadded();
      await load();
    } catch (e) {
      if (isUnauthorized(e)) {
        onauthlost();
        return;
      }
      if (e instanceof ApiError && e.status === 409) {
        await load();
      } else {
        showToast(t('common.genericError'));
      }
    } finally {
      busy[server.id] = false;
    }
  }

  onMount(() => {
    void load();
    const timer = setInterval(() => void load(), REFRESH_MS);
    return () => clearInterval(timer);
  });
</script>

<Modal title={$tr('dash.addServer')} labelId="add-server-title" {onclose}>
  <section class="block">
    <h3 class="section-title">{$tr('dash.available')}</h3>
    {#if !loaded}
      <p class="muted">{$tr('common.loading')}</p>
    {:else if loadFailed}
      <p class="error" role="alert">{$tr('common.networkError')}</p>
    {:else if available.length === 0}
      <p class="muted small">{$tr('dash.availableEmpty')}</p>
    {:else}
      <ul class="list">
        {#each available as server (server.id)}
          <li class="card item">
            <div class="info">
              <strong class="mono">{server.name}</strong>
              <span class="muted small mono">{server.address} · {server.os}</span>
            </div>
            <button class="btn" type="button" disabled={busy[server.id]} onclick={() => void add(server)}
              >{$tr('dash.add')}</button>
          </li>
        {/each}
      </ul>
    {/if}
  </section>

  <section class="block pairing">
    <h3 class="section-title">{$tr('dash.pending')}</h3>
    <div class="row">
      <span class="muted small">{$tr('dash.pendingCount', { n: pendingCount })}</span>
      <button class="btn primary" type="button" onclick={onpairing}>{$tr('dash.pendingOpen')}</button>
    </div>
  </section>
</Modal>

<style>
  .block {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  .section-title {
    margin: 0;
    font-size: 15px;
    color: var(--muted);
    text-transform: uppercase;
    letter-spacing: 0.06em;
  }

  .list {
    margin: 0;
    padding: 0;
    list-style: none;
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  .item {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 10px 12px;
    box-shadow: 3px 3px 0 var(--shade);
  }

  .item .btn {
    flex: none;
    padding: 4px 10px;
    font-size: 13px;
  }

  .info {
    display: flex;
    flex-direction: column;
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .pairing {
    border-top: 2px dashed var(--seg-off);
    padding-top: 16px;
  }
</style>
