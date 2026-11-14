<script lang="ts">
  import { onMount } from 'svelte';
  import { api, isUnauthorized, type Server } from '../api';
  import { locale, tr } from '../i18n';
  import DeleteServerModal from './DeleteServerModal.svelte';
  import EnrollModal from './EnrollModal.svelte';
  import { showToast } from './toast';

  let { onauthlost }: { onauthlost: () => void } = $props();

  const REFRESH_MS = 5000;

  let servers = $state<Server[]>([]);
  let loaded = $state(false);
  let loadFailed = $state(false);
  let enrollOpen = $state(false);
  let deleting = $state<Server | null>(null);

  async function refresh(): Promise<void> {
    try {
      servers = await api.servers();
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

  function formatDate(value: string, lang: string): string {
    const date = new Date(value);
    return Number.isNaN(date.getTime()) ? value : date.toLocaleString(lang);
  }

  onMount(() => {
    void refresh();
    const timer = setInterval(() => void refresh(), REFRESH_MS);
    return () => clearInterval(timer);
  });
</script>

<section class="servers-page">
  <div class="section-head">
    <h2>{$tr('servers.title')}</h2>
    <button class="btn primary" type="button" onclick={() => (enrollOpen = true)}>
      {$tr('servers.add')}
    </button>
  </div>

  {#if !loaded}
    <p class="muted">{$tr('common.loading')}</p>
  {:else if loadFailed && servers.length === 0}
    <p class="error" role="alert">{$tr('servers.loadFailed')}</p>
  {:else if servers.length === 0}
    <p class="card muted">{$tr('servers.empty')}</p>
  {:else}
    <ul class="server-list">
      {#each servers as server (server.id)}
        <li class="card server">
          <span class="dot" class:online={server.online} aria-hidden="true"></span>
          <div class="info">
            <h3>{server.name}</h3>
            <p class="muted mono">{server.address} · {server.os} · {server.agent_version}</p>
            <p class="small muted">
              {server.online ? $tr('servers.online') : $tr('servers.offline')}
              {#if !server.online && server.last_seen_at}
                · {$tr('servers.lastSeen', { date: formatDate(server.last_seen_at, $locale) })}
              {/if}
            </p>
          </div>
          <button class="btn" type="button" onclick={() => (deleting = server)}>
            {$tr('servers.delete')}
          </button>
        </li>
      {/each}
    </ul>
  {/if}
</section>

{#if enrollOpen}
  <EnrollModal
    onclose={() => (enrollOpen = false)}
    onchanged={() => void refresh()}
    {onauthlost}
  />
{/if}

{#if deleting}
  <DeleteServerModal
    server={deleting}
    onclose={() => (deleting = null)}
    ondeleted={() => {
      deleting = null;
      showToast($tr('servers.deleted'));
      void refresh();
    }}
    {onauthlost}
  />
{/if}

<style>
  .servers-page {
    max-width: 760px;
  }

  .section-head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    margin-bottom: 16px;
  }

  .section-head h2 {
    margin: 0;
    font-size: 22px;
  }

  .server-list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 16px;
  }

  .server {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 14px;
  }

  .dot {
    flex: none;
    width: 14px;
    height: 14px;
    border: 2px solid var(--line);
    background: var(--muted);
  }

  .dot.online {
    background: var(--good);
  }

  .info {
    flex: 1 1 220px;
    min-width: 0;
  }

  .info h3 {
    margin: 0 0 4px;
    font-size: 17px;
    overflow-wrap: anywhere;
  }

  .info p {
    margin: 0;
  }
</style>
