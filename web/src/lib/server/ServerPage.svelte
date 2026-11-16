<script lang="ts">
  import { api, isUnauthorized, type Role, type ServerDetail, type Sample } from '../../api';
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
  import DiskTab from './tabs/DiskTab.svelte';
  import NetworkTab from './tabs/NetworkTab.svelte';
  import FirewallTab from './tabs/FirewallTab.svelte';
  import SecurityTab from './tabs/SecurityTab.svelte';
  import UsersTab from './tabs/UsersTab.svelte';
  import DockerTab from './tabs/DockerTab.svelte';
  import { formatDateTime } from './format';
  import AgentUpdateBanner from './AgentUpdateBanner.svelte';
  import SideNav from './SideNav.svelte';
  import SectionsSheet from './SectionsSheet.svelte';
  import Modal from '../Modal.svelte';
  import PixIcon from '../ui/PixIcon.svelte';
  import TabBar, { type TabBarItem } from '../ui/TabBar.svelte';
  import { groupTabs, HISTORY_LIMIT, visibleTabs, type LiveSample, type TabId } from './model';

  let {
    name,
    tab: requestedTab = null,
    ontab,
    onBack,
    onagentchanged,
    role = 'admin',
    userModules = [],
  }: {
    name: string;
    /** Tab from the URL hash; unknown or hidden tabs fall back to the overview. */
    tab?: string | null;
    ontab?: (tab: TabId) => void;
    onBack: () => void;
    /** Called when an agent update of this server finished (refreshes the header counter). */
    onagentchanged?: () => void;
    /** An operator only gets the tabs of the modules that were given, an auditor none. */
    role?: Role;
    userModules?: string[];
  } = $props();

  let detail = $state<ServerDetail | null>(null);
  let status = $state<'loading' | 'ready' | 'missing' | 'error'>('loading');
  let live = $state<LiveSample[]>([]);
  let deleteOpen = $state(false);
  let attempt = $state(0);
  let sectionsOpen = $state(false);
  let menuOpen = $state(false);

  const serverId = $derived(detail?.id ?? null);
  const sample = $derived<Sample | null>(detail?.sample ?? null);
  const online = $derived(detail?.online ?? false);
  const isAdmin = $derived(role === 'admin');
  // a terminal as a user needs the terminal module too, on the agent and in the rights of the operator
  const canTerminal = $derived((detail?.modules ?? []).includes('terminal') && (isAdmin || (role === 'operator' && userModules.includes('terminal'))));
  const tabs = $derived(visibleTabs(detail?.modules ?? [], isAdmin ? null : role === 'operator' ? userModules : []));
  const tab = $derived<TabId>(tabs.find((x) => x.id === requestedTab)?.id ?? 'overview');
  const currentTabKey = $derived(tabs.find((x) => x.id === tab)?.key ?? 'srv.tab.overview');
  const currentGroupKey = $derived(groupTabs(tabs).find((g) => g.tabs.some((x) => x.id === tab))?.key ?? 'srv.group.general');

  /** Phone: the tabs that sit in the bottom bar; everything else is under "Sections". */
  const PINNED: TabId[] = ['overview', 'terminal', 'files', 'services'];
  const barTabs = $derived.by(() => {
    const ids = tabs.map((x) => x.id);
    const picked = PINNED.filter((id) => ids.includes(id));
    for (const id of ids) if (picked.length < 4 && !picked.includes(id)) picked.push(id);
    return picked.slice(0, 4);
  });
  const badges = $derived<Partial<Record<TabId, { text: string; tone: 'warn' | 'crit' }>>>({
    ...((sample?.updates ?? 0) > 0 ? { updates: { text: String(sample?.updates), tone: (sample?.security_updates ?? 0) > 0 ? 'crit' : 'warn' } } : {}),
    ...((sample?.failed_services ?? 0) > 0 ? { services: { text: String(sample?.failed_services), tone: 'crit' } } : {}),
  } as Partial<Record<TabId, { text: string; tone: 'warn' | 'crit' }>>);
  const barItems = $derived<TabBarItem[]>([
    ...barTabs.map((id) => ({
      id,
      label: $tr(tabs.find((x) => x.id === id)?.key ?? ''),
      icon: id,
      current: tab === id,
      badge: badges[id]?.text,
      tone: badges[id]?.tone,
    })),
    { id: '#sections', label: $tr('nav.sections'), icon: 'more', current: !barTabs.includes(tab) },
  ]);

  function onBar(id: string): void {
    if (id === '#sections') sectionsOpen = true;
    else ontab?.(id as TabId);
  }

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

<section class="srv" data-tab={tab}>
  <!-- Phone header: back, the section as a title, server and its state under it, the actions in a sheet. -->
  <header class="phead">
    <button class="btn ic" type="button" aria-label={$tr('srv.back')} onclick={onBack}><PixIcon name="back" /></button>
    <div class="pt">
      <h1>{detail ? $tr(currentTabKey) : name}</h1>
      <p class="mono">
        {#if detail}<span class="sq" class:online aria-hidden="true"></span>{detail.name} · {detail.address} · {online ? $tr('srv.online') : $tr('srv.offline')}{:else}{name}{/if}
      </p>
    </div>
    {#if detail && isAdmin}
      <button class="btn ic" type="button" aria-label={$tr('srv.menu')} onclick={() => (menuOpen = true)}><PixIcon name="more" /></button>
    {/if}
  </header>
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
          {#if isAdmin}
            <RebootButton server={detail} />
            <button class="btn danger" type="button" onclick={() => (deleteOpen = true)}>{$tr('srv.delete')}</button>
          {/if}
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
    {#if isAdmin}<AgentUpdateBanner server={detail} onchanged={onagentchanged} />{/if}

    {#if sample?.reboot_required}
      <p class="banner" role="status">{$tr('srv.rebootRequired')}</p>
    {/if}

    <div class="body">
      <SideNav
        {tabs}
        current={tab}
        onselect={(id) => ontab?.(id)}
        updates={sample?.updates ?? 0}
        securityUpdates={sample?.security_updates ?? 0}
        failedServices={sample?.failed_services ?? 0}
        agentVersion={detail.agent_version ? `agent ${detail.agent_version}` : ''}
      />
      <div class="main">
        <div class="crumb">{$tr(currentGroupKey)}</div>
        <h2 class="page-title">{$tr(currentTabKey)}</h2>
    <div class="tabpanel" role="tabpanel">
      {#if tab === 'overview'}
        <OverviewTab server={detail} {sample} {live} {online} />
      {:else if tab === 'metrics'}
        <MetricsTab serverId={detail.id} />
      {:else if tab === 'files'}
        <FilesTab server={detail} />
      {:else if tab === 'disk'}
        <DiskTab server={detail} />
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
      {:else if tab === 'security'}
        <SecurityTab server={detail} />
      {:else if tab === 'users'}
        <UsersTab server={detail} canTerminal={canTerminal} />
      {:else if tab === 'logs'}
        <LogsTab server={detail} />
      {:else}
        <p class="soon">{$tr('srv.soon')}</p>
      {/if}
    </div>
      </div>
    </div>
  {/if}
</section>

{#if detail}
  <TabBar items={barItems} label={$tr('srv.tabsLabel')} onselect={onBar} />
{/if}

{#if sectionsOpen}
  <SectionsSheet
    {tabs}
    current={tab}
    {badges}
    onclose={() => (sectionsOpen = false)}
    onselect={(id) => {
      sectionsOpen = false;
      ontab?.(id);
    }} />
{/if}

{#if menuOpen && detail}
  <Modal title={detail.name} labelId="srv-menu-title" onclose={() => (menuOpen = false)}>
    <RebootButton server={detail} />
    <button class="btn danger" type="button" onclick={() => { menuOpen = false; deleteOpen = true; }}>{$tr('srv.delete')}</button>
  </Modal>
{/if}

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
    max-width: 1440px;
    margin: 0 auto;
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
    font-variation-settings: "ELSH" 40;
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

  /* Sidebar with the tabs on the left, the panel on the right. */
  .body {
    display: flex;
    align-items: flex-start;
    gap: 20px;
    margin-top: 6px;
  }

  .main {
    flex: 1 1 auto;
    min-width: 0;
  }

  .crumb {
    font-family: var(--font-display);
    font-size: 12px;
    letter-spacing: 0.1em;
    text-transform: uppercase;
    color: var(--muted);
  }

  .page-title {
    margin: 2px 0 12px;
    font-family: var(--font-display);
    font-size: 26px;
    font-weight: 400;
    line-height: 1.2;
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

  .phead {
    display: none;
  }

  @media (max-width: 860px) {
    .body {
      flex-direction: column;
      align-items: stretch;
      gap: 14px;
    }
  }

  @media (max-width: 720px) {
    .srv {
      gap: 12px;
      padding-bottom: calc(var(--bar-h) + var(--safe-b) + 16px);
    }

    /* One header (the global bar is hidden on a server page); the side menu became the bottom bar. */
    .titlebar,
    .crumb,
    .page-title {
      display: none;
    }

    .phead {
      position: sticky;
      top: 0;
      z-index: 10;
      display: flex;
      align-items: center;
      gap: 8px;
      min-height: 56px;
      padding: 5px 12px 5px 8px;
      padding-top: calc(5px + var(--safe-t));
      background: var(--surface);
      border-bottom: 2px solid var(--line);
    }

    .phead .btn {
      flex: none;
      width: 44px;
      padding: 0;
    }

    .phead .btn:first-child {
      border-color: transparent;
      box-shadow: none;
      background: none;
    }

    .pt {
      flex: 1;
      min-width: 0;
    }

    .pt h1 {
      overflow: hidden;
      margin: 0;
      font-family: var(--font-display);
      font-size: 22px;
      line-height: 1.15;
      text-overflow: ellipsis;
      white-space: nowrap;
    }

    .pt p {
      display: flex;
      align-items: center;
      gap: 6px;
      overflow: hidden;
      margin: 0;
      color: var(--muted);
      font-size: 11.5px;
      white-space: nowrap;
    }

    .pt .sq {
      flex: none;
      width: 9px;
      height: 9px;
      background: var(--muted);
    }

    .pt .sq.online {
      background: var(--good);
    }

    .srv > :global(:not(.phead)) {
      margin-left: 12px;
      margin-right: 12px;
    }

    .body {
      margin-top: 0;
    }

    /* No frame around the tab: its blocks are the cards, so the width is not spent twice. */
    .tabpanel {
      padding: 0;
      background: none;
      border: 0;
      box-shadow: none;
    }
  }
</style>
