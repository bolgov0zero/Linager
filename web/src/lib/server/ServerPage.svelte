<script lang="ts">
  import { api, isUnauthorized, type ServerDetail, type Sample } from '../../api';
  import { locale, tr } from '../../i18n';
  import { subscribeLive } from '../../live';
  import DeleteServerModal from '../DeleteServerModal.svelte';
  import { showToast } from '../toast';
  import MetricsTab from './MetricsTab.svelte';
  import OverviewTab from './OverviewTab.svelte';
  import TerminalTab from './tabs/TerminalTab.svelte';
  import LogsTab from './tabs/LogsTab.svelte';
  import ProcessesTab from './tabs/ProcessesTab.svelte';
  import ServicesTab from './tabs/ServicesTab.svelte';
  import UpdatesTab from './tabs/UpdatesTab.svelte';
  import RebootButton from './tabs/RebootButton.svelte';
  import FilesTab from './tabs/FilesTab.svelte';
  import NetworkTab from './tabs/NetworkTab.svelte';
  import FirewallTab from './tabs/FirewallTab.svelte';
  import DockerTab from './tabs/DockerTab.svelte';
  import { formatDateTime } from './format';
  import { HISTORY_LIMIT, TABS, type LiveSample, type TabId } from './model';

  let { name, onBack }: { name: string; onBack: () => void } = $props();

  let detail = $state<ServerDetail | null>(null);
  let status = $state<'loading' | 'ready' | 'missing' | 'error'>('loading');
  let tab = $state<TabId>('overview');
  let live = $state<LiveSample[]>([]);
  let deleteOpen = $state(false);
  let attempt = $state(0);

  const serverId = $derived(detail?.id ?? null);
  const sample = $derived<Sample | null>(detail?.sample ?? null);
  const online = $derived(detail?.online ?? false);

  // Resolve the server by name, then load its detail. Reruns on name change or retry.
  $effect(() => {
    const wanted = name;
    void attempt;
    let active = true;
    status = 'loading';
    detail = null;
    live = [];

    (async () => {
      try {
        const list = await api.servers();
        const found = list.find((s) => s.name === wanted);
        if (!found) {
          if (active) status = 'missing';
          return;
        }
        const d = await api.server(found.id);
        if (!active) return;
        detail = d;
        live = d.spark.map((cpu) => ({ cpu, mem: null, rx: null, tx: null }));
        status = 'ready';
      } catch (e) {
        if (!active) return;
        if (isUnauthorized(e)) {
          onBack();
          return;
        }
        status = 'error';
      }
    })();

    return () => {
      active = false;
    };
  });

  // Live updates for this server only. Depends on the id, not on the detail object,
  // so the shared connection is not re-subscribed on every sample.
  $effect(() => {
    const id = serverId;
    if (!id) return;
    return subscribeLive((e) => {
      if (e.type === 'dashboard' || e.server_id !== id || !detail) return;
      if (e.type === 'metrics') {
        const s = e.sample;
        // Mutate in place: tabs depend on individual fields (id, online); replacing the object would
        // restart their streams and polling on every sample.
        detail.online = true;
        detail.sample = s;
        live = [...live, { cpu: s.cpu, mem: s.mem_pct, rx: s.rx_bps, tx: s.tx_bps }].slice(-HISTORY_LIMIT);
      } else if (e.type === 'status') {
        detail.online = e.online;
        detail.last_seen_at = e.last_seen_at;
      }
    });
  });
</script>

<section class="srv">
  <header class="titlebar">
    <div class="title-row">
      <button class="btn" type="button" onclick={onBack}>{$tr('srv.back')}</button>
      <div class="name">
        {#if detail}
          <span class="dot" class:online aria-hidden="true"></span>
        {/if}
        <h1>{detail?.name ?? name}</h1>
        {#if detail}
          <span class="state" class:online>{online ? $tr('srv.online') : $tr('srv.offline')}</span>
        {/if}
      </div>
      {#if detail}
        <div class="row actions">
          <RebootButton server={detail} />
          <button class="btn danger" type="button" onclick={() => (deleteOpen = true)}>{$tr('srv.delete')}</button>
        </div>
      {/if}
    </div>
    {#if detail}
      {#if !online && detail.last_seen_at}
        <p class="muted small">{$tr('srv.lastSeen', { date: formatDateTime(detail.last_seen_at, $locale) })}</p>
      {/if}
      <p class="muted small mono">
        {$tr('srv.subline', {
          os: detail.system?.os ?? detail.os,
          address: detail.address,
          version: detail.agent_version,
        })}
      </p>
    {/if}
  </header>

  {#if status === 'loading'}
    <p class="muted">{$tr('common.loading')}</p>
  {:else if status === 'missing'}
    <p class="card">{$tr('srv.notFound')}</p>
  {:else if status === 'error' || !detail}
    <div class="card state">
      <p class="error" role="alert">{$tr('srv.loadFailed')}</p>
      <div class="row">
        <button class="btn" type="button" onclick={() => attempt++}>{$tr('srv.retry')}</button>
      </div>
    </div>
  {:else}
    {#if sample?.reboot_required}
      <p class="banner" role="status">{$tr('srv.rebootRequired')}</p>
    {/if}

    <div class="tabs" role="tablist" aria-label={$tr('srv.tabsLabel')}>
      {#each TABS as t (t.id)}
        <button
          class="tab"
          type="button"
          role="tab"
          aria-selected={tab === t.id}
          onclick={() => (tab = t.id)}
        >
          {$tr(t.key)}
        </button>
      {/each}
    </div>

    <div class="tabpanel" role="tabpanel">
      {#if tab === 'overview'}
        <OverviewTab server={detail} {sample} {live} {online} />
      {:else if tab === 'metrics'}
        <MetricsTab serverId={detail.id} />
      {:else if tab === 'files'}
        <FilesTab server={detail} />
      {:else if tab === 'network'}
        <NetworkTab server={detail} />
      {:else if tab === 'firewall'}
        <FirewallTab server={detail} />
      {:else if tab === 'docker'}
        <DockerTab server={detail} />
      {:else if tab === 'terminal'}
        <TerminalTab server={detail} />
      {:else if tab === 'services'}
        <ServicesTab server={detail} />
      {:else if tab === 'updates'}
        <UpdatesTab server={detail} />
      {:else if tab === 'processes'}
        <ProcessesTab server={detail} />
      {:else if tab === 'logs'}
        <LogsTab server={detail} />
      {:else}
        <p class="soon">{$tr('srv.soon')}</p>
      {/if}
    </div>
  {/if}
</section>

{#if deleteOpen && detail}
  <DeleteServerModal
    server={detail}
    onclose={() => (deleteOpen = false)}
    ondeleted={() => {
      deleteOpen = false;
      showToast($tr('srv.deleted'));
      onBack();
    }}
    onauthlost={onBack}
  />
{/if}

<style>
  .srv {
    display: flex;
    flex-direction: column;
    gap: 12px;
    max-width: 1200px;
  }

  .titlebar {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .title-row {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 12px;
  }

  .name {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 10px;
    min-width: 0;
    flex: 1 1 auto;
  }

  .titlebar h1 {
    margin: 0;
    font-size: 30px;
    line-height: 1.1;
    overflow-wrap: anywhere;
  }

  .actions {
    margin-left: auto;
  }

  .dot {
    flex: none;
    width: 12px;
    height: 12px;
    background: var(--muted);
  }

  .dot.online {
    background: var(--good);
  }

  /* Status pill next to the server name, like the design's "в норме" tag. */
  .state {
    padding: 1px 8px;
    border: 2px solid var(--muted);
    color: var(--muted);
    font-size: 12px;
    line-height: 1.4;
  }

  .state.online {
    border-color: var(--good);
    color: var(--good);
  }

  .banner {
    margin: 0;
    padding: 8px 12px;
    border: 2px solid var(--warn);
    color: var(--warn);
  }

  .state {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 12px;
  }

  /* Horizontally scrollable tab bar; active tab sits on the panel's top border. */
  .tabs {
    display: flex;
    gap: 4px;
    overflow-x: auto;
    scrollbar-width: thin;
    margin-top: 12px;
    margin-bottom: -2px;
    padding-bottom: 0;
  }

  .tab {
    flex: none;
    position: relative;
    padding: 8px 14px;
    background: none;
    border: 2px solid transparent;
    border-bottom: 0;
    color: var(--muted);
    cursor: pointer;
    white-space: nowrap;
  }

  .tab[aria-selected='true'] {
    color: var(--ink);
    background: var(--surface);
    border-color: var(--line);
    z-index: 1;
  }

  .tabpanel {
    background: var(--surface);
    border: 2px solid var(--line);
    box-shadow: 4px 4px 0 var(--shade);
    padding: 16px;
    min-width: 0;
  }

  .soon {
    margin: 0;
    padding: 48px 0;
    text-align: center;
    color: var(--muted);
    font-family: var(--font-display);
    font-size: 20px;
  }

  @media (max-width: 600px) {
    .titlebar h1 {
      font-size: 24px;
    }

    .tabpanel {
      padding: 12px;
    }
  }
</style>
