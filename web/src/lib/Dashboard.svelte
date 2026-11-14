<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import { api, isUnauthorized, type Group, type LiveEvent, type ServerCard } from '../api';
  import { locale, t, tr } from '../i18n';
  import { subscribeLive } from '../live';
  import CardView from './dashboard/CardView.svelte';
  import { pluralForm } from './dashboard/format';
  import GroupsModal from './dashboard/GroupsModal.svelte';
  import Summary from './dashboard/Summary.svelte';
  import { levelOf, summarize, type AgentFlag } from './dashboard/status';
  import ContextMenu, { type MenuItem } from './ui/ContextMenu.svelte';
  import Select from './ui/Select.svelte';
  import { openTermWindow } from './terminal/windows';
  import { showToast } from './toast';

  type Filter = 'all' | 'problems' | 'offline';
  type View = 'cards' | 'map';

  let {
    editing,
    reloadKey,
    onopen,
    onauthlost,
  }: {
    editing: boolean;
    /** Bumped by the parent after the server list changed (add or pair). */
    reloadKey: number;
    onopen: (name: string, tab?: string) => void;
    onauthlost: () => void;
  } = $props();

  const VIEW_KEY = 'linager.dashboard.view';
  const GROUP_KEY = 'linager.dashboard.group';
  const LIVE_RELOAD_MS = 500;
  const SAFETY_RELOAD_MS = 60_000;
  const TICK_MS = 15_000;
  const ORDER_SAVE_MS = 400;
  const UNDO_MS = 5_000;

  function readView(): View {
    try {
      const value = localStorage.getItem(VIEW_KEY);
      if (value === 'cards') return value;
      if (value === 'map' || value === 'rows') return 'map'; // the old rows view was replaced by the map
    } catch {
      // storage unavailable: fall back to the default view
    }
    return 'cards';
  }

  function storeView(next: View): void {
    try {
      localStorage.setItem(VIEW_KEY, next);
    } catch {
      // storage unavailable: the choice just is not remembered
    }
  }

  function readGroup(): string {
    try {
      return localStorage.getItem(GROUP_KEY) ?? 'all';
    } catch {
      return 'all';
    }
  }

  function pickGroup(next: string): void {
    groupFilter = next;
    try {
      localStorage.setItem(GROUP_KEY, next);
    } catch {
      // storage unavailable: the choice just is not remembered
    }
  }

  let items = $state<ServerCard[]>([]);
  let groups = $state<Group[]>([]);
  /** 'all', 'none' (servers without a group) or a group id. */
  let groupFilter = $state(readGroup());
  let groupsOpen = $state(false);
  /** Agent update state per server id, from the same endpoint as the updates page. */
  let agentFlags = $state<Record<string, AgentFlag>>({});
  let loaded = $state(false);
  let loadFailed = $state(false);
  let filter = $state<Filter>('all');
  let query = $state('');
  let view = $state<View>(readView());
  let now = $state(Date.now());
  /** A card removed from the dashboard whose API call waits for the undo window. */
  let pending = $state<{ card: ServerCard; index: number } | null>(null);
  let dragId = $state<string | null>(null);
  let overId = $state<string | null>(null);

  /** Only the server list scrolls: its height is what is left of the window below the summary and the filters. */
  let scroller = $state<HTMLDivElement | undefined>(undefined);
  let listH = $state(480);
  function fitList(): void {
    if (!scroller) return;
    const top = scroller.getBoundingClientRect().top + window.scrollY;
    listH = Math.max(240, Math.floor(window.innerHeight - top - 28));
  }
  $effect(() => {
    // Measure again when the list appears, changes its mode or something above it changes its height.
    void [scroller, view, visible.length, loaded, totals];
    fitList();
    window.addEventListener('resize', fitList);
    return () => window.removeEventListener('resize', fitList);
  });

  let seenReload = -1;
  let reloadTimer: ReturnType<typeof setTimeout> | undefined;
  let orderTimer: ReturnType<typeof setTimeout> | undefined;
  let undoTimer: ReturnType<typeof setTimeout> | undefined;

  const inGroup = $derived(
    items.filter((card) =>
      groupFilter === 'all' ? true : groupFilter === 'none' ? card.group_id === null : String(card.group_id) === groupFilter,
    ),
  );
  const totals = $derived(summarize(inGroup));
  const countLabel = $derived($tr(`dash.servers.${pluralForm($locale, inGroup.length)}`, { n: inGroup.length }));
  const groupOptions = $derived([
    { value: 'all', label: $tr('dash.group.all') },
    { value: 'none', label: $tr('dash.group.none') },
    ...groups.map((g) => ({ value: String(g.id), label: g.name })),
  ]);
  const cardGroupOptions = $derived([
    { value: '0', label: $tr('dash.group.none') },
    ...groups.map((g) => ({ value: String(g.id), label: g.name })),
  ]);
  const visible = $derived.by(() => {
    const q = query.trim().toLowerCase();
    return inGroup.filter((card) => {
      if (filter === 'offline' && card.online) return false;
      if (filter === 'problems' && levelOf(card) === 'ok') return false;
      if (!q) return true;
      return card.name.toLowerCase().includes(q) || card.address.toLowerCase().includes(q);
    });
  });

  function indexOf(id: string): number {
    return items.findIndex((c) => c.id === id);
  }

  function visibleIndex(id: string): number {
    return visible.findIndex((c) => c.id === id);
  }

  async function setCardGroup(card: ServerCard, value: string): Promise<void> {
    const groupId = value === '0' ? null : Number(value);
    try {
      await api.serverSetGroup(card.id, groupId);
      items = items.map((c) => (c.id === card.id ? { ...c, group_id: groupId } : c));
    } catch (e) {
      if (isUnauthorized(e)) {
        onauthlost();
        return;
      }
      showToast(t('dash.group.setFailed'));
    }
  }

  async function loadAgentFlags(): Promise<void> {
    try {
      const info = await api.updates();
      const next: Record<string, AgentFlag> = {};
      for (const agent of info.agents) {
        if (agent.status === 'available' || agent.status === 'unsupported') next[agent.id] = agent.status;
      }
      agentFlags = next;
    } catch {
      // the tags are optional: keep the previous state
    }
  }

  async function load(): Promise<void> {
    void loadAgentFlags();
    try {
      const [res, list] = await Promise.all([api.dashboard(), api.groups()]);
      const hidden = pending?.card.id;
      items = hidden ? res.items.filter((c) => c.id !== hidden) : res.items;
      groups = list;
      if (groupFilter !== 'all' && groupFilter !== 'none' && !list.some((g) => String(g.id) === groupFilter)) pickGroup('all');
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

  function scheduleLoad(): void {
    clearTimeout(reloadTimer);
    reloadTimer = setTimeout(() => void load(), LIVE_RELOAD_MS);
  }

  function replaceAt(index: number, card: ServerCard): void {
    items = items.map((c, i) => (i === index ? card : c));
  }

  function onLive(event: LiveEvent): void {
    if (event.type === 'dashboard') {
      scheduleLoad();
      return;
    }
    const index = indexOf(event.server_id);
    const current = items[index];
    if (index < 0 || !current) {
      scheduleLoad();
      return;
    }
    if (event.type === 'metrics') {
      replaceAt(index, {
        ...current,
        online: true,
        last_seen_at: new Date().toISOString(),
        sample: event.sample,
        spark: [...current.spark, event.sample.cpu].slice(-12),
      });
    } else if (event.type === 'status') {
      replaceAt(index, { ...current, online: event.online, last_seen_at: event.last_seen_at });
    }
  }

  // Reload when the parent says the server list changed; the first run loads the list.
  $effect(() => {
    if (reloadKey !== seenReload) {
      seenReload = reloadKey;
      void load();
    }
  });

  onMount(() => {
    const unsubscribe = subscribeLive(onLive);
    const safety = setInterval(() => void load(), SAFETY_RELOAD_MS);
    const ticker = setInterval(() => (now = Date.now()), TICK_MS);
    return () => {
      unsubscribe();
      clearInterval(safety);
      clearInterval(ticker);
    };
  });

  onDestroy(() => {
    clearTimeout(reloadTimer);
    if (pending) void commitRemoval();
    if (orderTimer) {
      clearTimeout(orderTimer);
      void persistOrder();
    }
  });

  function setView(next: View): void {
    view = next;
    storeView(next);
  }

  // ---- context menu of a map tile ----

  let menu = $state<{ x: number; y: number; items: MenuItem[] } | null>(null);

  // Long press (touch and pen) opens the same menu; the click that follows the press must not open the server.
  const LONG_PRESS_MS = 500;
  const PRESS_SLOP = 10;
  let pressTimer: ReturnType<typeof setTimeout> | undefined;
  let pressed = false;

  function pressStart(event: PointerEvent, card: ServerCard): void {
    pressed = false;
    if (event.pointerType === 'mouse' || editing) return;
    const x = event.clientX;
    const y = event.clientY;
    clearTimeout(pressTimer);
    pressTimer = setTimeout(() => {
      pressed = true;
      showMenu(x, y, card);
    }, LONG_PRESS_MS);
    const cancel = (e: Event) => {
      if (e instanceof PointerEvent && e.type === 'pointermove' && Math.hypot(e.clientX - x, e.clientY - y) < PRESS_SLOP) return;
      clearTimeout(pressTimer);
      window.removeEventListener('pointermove', cancel);
      window.removeEventListener('pointerup', cancel);
      window.removeEventListener('pointercancel', cancel);
    };
    window.addEventListener('pointermove', cancel);
    window.addEventListener('pointerup', cancel);
    window.addEventListener('pointercancel', cancel);
  }

  function tileClick(card: ServerCard): void {
    if (pressed) {
      pressed = false;
      return;
    }
    onopen(card.name);
  }

  function openMenu(event: MouseEvent, card: ServerCard): void {
    event.preventDefault();
    showMenu(event.clientX, event.clientY, card);
  }

  function showMenu(x: number, y: number, card: ServerCard): void {
    const items: MenuItem[] = [
      { key: 'term', label: $tr('dash.terminal'), disabled: !card.online, onselect: () => openTermWindow(card.id, card.name) },
      { key: 'open', label: $tr('dash.open'), onselect: () => onopen(card.name) },
    ];
    if (groups.length > 0) {
      for (const g of groups) {
        items.push({
          key: `g${g.id}`,
          label: `${card.group_id === g.id ? '✓ ' : ''}${$tr('dash.group.label')}: ${g.name}`,
          separated: items.length === 2,
          onselect: () => void setCardGroup(card, String(g.id)),
        });
      }
      if (card.group_id !== null) {
        items.push({ key: 'g0', label: `${$tr('dash.group.label')}: ${$tr('dash.group.none')}`, onselect: () => void setCardGroup(card, '0') });
      }
    }
    items.push({ key: 'remove', label: $tr('dash.remove'), danger: true, separated: true, onselect: () => void removeCard(card) });
    menu = { x, y, items };
  }

  // ---- ordering ----

  function moveTo(fromId: string, toId: string): void {
    const from = indexOf(fromId);
    const to = indexOf(toId);
    if (from < 0 || to < 0 || from === to) return;
    const next = items.slice();
    const [moved] = next.splice(from, 1);
    if (!moved) return;
    next.splice(to, 0, moved);
    items = next;
    scheduleOrderSave();
  }

  /** Moves a card one step among the cards currently shown (the selected group), swapping with its visible neighbour. */
  function move(id: string, dir: -1 | 1): void {
    const index = indexOf(id);
    const neighbour = visible[visibleIndex(id) + dir];
    const target = neighbour ? indexOf(neighbour.id) : -1;
    if (index < 0 || target < 0) return;
    const next = items.slice();
    const a = next[index];
    const b = next[target];
    if (!a || !b) return;
    next[index] = b;
    next[target] = a;
    items = next;
    scheduleOrderSave();
  }

  function scheduleOrderSave(): void {
    clearTimeout(orderTimer);
    orderTimer = setTimeout(() => void persistOrder(), ORDER_SAVE_MS);
  }

  async function persistOrder(): Promise<void> {
    orderTimer = undefined;
    await commitRemoval();
    try {
      await api.dashboardOrder(items.map((c) => c.id));
    } catch (e) {
      if (isUnauthorized(e)) {
        onauthlost();
        return;
      }
      showToast(t('dash.orderFailed'));
      void load();
    }
  }

  // ---- removal with undo ----

  async function removeCard(card: ServerCard): Promise<void> {
    const index = indexOf(card.id);
    if (index < 0) return;
    await commitRemoval();
    const at = indexOf(card.id);
    if (at < 0) return;
    items = items.filter((c) => c.id !== card.id);
    pending = { card, index: at };
    clearTimeout(undoTimer);
    undoTimer = setTimeout(() => void commitRemoval(), UNDO_MS);
  }

  /** Sends the delayed removal to the API once the undo window has passed. */
  async function commitRemoval(): Promise<void> {
    const current = pending;
    if (!current) return;
    clearTimeout(undoTimer);
    undoTimer = undefined;
    pending = null;
    try {
      await api.dashboardRemove(current.card.id);
    } catch (e) {
      if (isUnauthorized(e)) {
        onauthlost();
        return;
      }
      showToast(t('dash.removeFailed'));
      void load();
    }
  }

  function undoRemoval(): void {
    const current = pending;
    if (!current) return;
    clearTimeout(undoTimer);
    undoTimer = undefined;
    pending = null;
    const next = items.slice();
    next.splice(Math.min(current.index, next.length), 0, current.card);
    items = next;
    scheduleOrderSave();
  }

  // ---- drag and drop (edit mode) ----

  function onDragStart(event: DragEvent, id: string): void {
    dragId = id;
    if (event.dataTransfer) {
      event.dataTransfer.effectAllowed = 'move';
      event.dataTransfer.setData('text/plain', id);
    }
  }

  function onDragOver(event: DragEvent, id: string): void {
    if (!dragId) return;
    event.preventDefault();
    if (event.dataTransfer) event.dataTransfer.dropEffect = 'move';
    overId = id;
  }

  function onDrop(event: DragEvent, id: string): void {
    event.preventDefault();
    if (dragId && dragId !== id) moveTo(dragId, id);
    dragId = null;
    overId = null;
  }

  function onDragEnd(): void {
    dragId = null;
    overId = null;
  }
</script>

<section class="dash">
  {#if !loaded}
    <p class="muted">{$tr('common.loading')}</p>
  {:else if loadFailed && items.length === 0}
    <p class="error" role="alert">{$tr('dash.loadFailed')}</p>
  {:else}
    <p class="muted small count">{countLabel}</p>

    <Summary summary={totals} />

    <div class="toolbar">
      <div class="group" role="group" aria-label={$tr('dash.filters')}>
        <button class="chip" type="button" aria-pressed={filter === 'all'} onclick={() => (filter = 'all')}
          >{$tr('dash.filter.all')}</button>
        <button
          class="chip"
          type="button"
          aria-pressed={filter === 'problems'}
          onclick={() => (filter = 'problems')}>{$tr('dash.filter.problems')}</button>
        <button
          class="chip"
          type="button"
          aria-pressed={filter === 'offline'}
          onclick={() => (filter = 'offline')}>{$tr('dash.filter.offline')}</button>
        {#if groups.length > 0}
          <Select size="sm" value={groupFilter} options={groupOptions} ariaLabel={$tr('dash.group.label')} onchange={pickGroup} />
        {/if}
        <button class="chip" type="button" onclick={() => (groupsOpen = true)}>{$tr('dash.group.manage')}</button>
      </div>
      <div class="group end">
        <button class="chip" type="button" aria-pressed={view === 'cards'} onclick={() => setView('cards')}
          >{$tr('dash.view.cards')}</button>
        <button class="chip" type="button" aria-pressed={view === 'map'} onclick={() => setView('map')}
          >{$tr('dash.view.map')}</button>
        <input
          class="input search"
          type="search"
          placeholder={$tr('dash.search')}
          aria-label={$tr('dash.search')}
          bind:value={query}
        />
      </div>
    </div>

    {#if items.length === 0 && !pending}
      <p class="card muted">{$tr('dash.noServers')}</p>
    {:else if visible.length === 0}
      <p class="muted">{$tr('dash.empty')}</p>
    {:else if view === 'cards'}
      <div class="scroll" bind:this={scroller} style:--list-h="{listH}px">
      <div class="grid">
        {#each visible as card (card.id)}
          <div
            class="slot"
            class:editing
            class:over={editing && overId === card.id && dragId !== card.id}
            class:dragging={dragId === card.id}
            draggable={editing}
            role="presentation"
            ondragstart={(e) => onDragStart(e, card.id)}
            ondragover={(e) => onDragOver(e, card.id)}
            ondrop={(e) => onDrop(e, card.id)}
            ondragend={onDragEnd}>
            <CardView
              {card}
              agent={agentFlags[card.id] ?? null}
              {now}
              {editing}
              index={visibleIndex(card.id)}
              count={visible.length}
              {onopen}
              onmove={(dir) => move(card.id, dir)}
              onremove={() => void removeCard(card)} />
            {#if editing && groups.length > 0}
              <div class="pick">
                <Select size="sm" value={String(card.group_id ?? 0)} options={cardGroupOptions} ariaLabel={$tr('dash.group.label')} onchange={(v) => void setCardGroup(card, v)} />
              </div>
            {/if}
          </div>
        {/each}
      </div>
      </div>
    {:else}
      <div class="scroll" bind:this={scroller} style:--list-h="{listH}px">
      <div class="map">
        {#each visible as card (card.id)}
          {@const level = levelOf(card)}
          {@const sample = card.online ? card.sample : null}
          <div
            class="tile {level}"
            class:editing
            class:over={editing && overId === card.id && dragId !== card.id}
            class:dragging={dragId === card.id}
            role="button"
            tabindex="0"
            draggable={editing}
            title="{card.address} · {card.os} · {$tr('dash.agent')} {card.agent_version}"
            onclick={() => tileClick(card)}
            onkeydown={(e) => e.key === 'Enter' && onopen(card.name)}
            onpointerdown={(e) => pressStart(e, card)}
            oncontextmenu={(e) => openMenu(e, card)}
            ondragstart={(e) => onDragStart(e, card.id)}
            ondragover={(e) => onDragOver(e, card.id)}
            ondrop={(e) => onDrop(e, card.id)}
            ondragend={onDragEnd}>
            <b class="tname">{card.name}</b>
            <span class="tsub">{sample ? `c ${Math.round(sample.cpu)}% · r ${Math.round(sample.mem_pct)}%` : $tr('dash.tag.offline')}</span>
          </div>
        {/each}
      </div>
      </div>
    {/if}
  {/if}
</section>

{#if menu}
  <ContextMenu x={menu.x} y={menu.y} items={menu.items} label={$tr('dash.menu')} onclose={() => (menu = null)} />
{/if}

{#if groupsOpen}
  <GroupsModal {groups} onclose={() => (groupsOpen = false)} onchanged={() => void load()} {onauthlost} />
{/if}

{#if pending}
  <div class="toast undo" role="status" aria-live="polite">
    <span>{$tr('dash.undo.text', { name: pending.card.name })}</span>
    <button class="btn" type="button" onclick={undoRemoval}>{$tr('dash.undo.button')}</button>
  </div>
{/if}

<style>
  .pick {
    margin-top: 8px;
  }

  .count {
    margin: 0 0 12px;
  }

  .toolbar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 10px 16px;
    margin: 16px 0;
  }

  .group {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
    min-width: 0;
  }

  .group.end {
    margin-left: auto;
  }

  .search {
    width: 260px;
    max-width: 100%;
    padding: 5px 10px;
  }

  /* On wide screens the list scrolls inside; the padding keeps the cards' offset shadows from being clipped.
     On phones the whole page scrolls as before. */
  @media (min-width: 721px) {
    .scroll {
      max-height: var(--list-h);
      overflow-y: auto;
      padding: 2px 8px 8px 2px;
      margin: -2px -8px -8px -2px;
    }
  }

  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(min(300px, 100%), 1fr));
    gap: 18px;
  }

  /* The status map: small tiles, the left edge shows the status. */
  .map {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(min(130px, 100%), 1fr));
    gap: 6px;
  }

  .tile {
    display: flex;
    flex-direction: column;
    min-width: 0;
    padding: 4px 8px;
    background: var(--surface);
    border: 2px solid var(--line);
    border-left-width: 6px;
    line-height: 1.35;
    cursor: pointer;
    outline: 2px dashed transparent;
    outline-offset: 3px;
    -webkit-touch-callout: none;
    -webkit-user-select: none;
    user-select: none;
  }

  .tile.ok {
    border-left-color: var(--good);
  }

  .tile.warn {
    border-left-color: var(--warn);
  }

  .tile.crit {
    border-left-color: var(--crit);
  }

  .tile.off {
    border-left-color: var(--muted);
    opacity: 0.65;
  }

  .tile:hover,
  .tile:focus-visible {
    background: var(--seg-off);
  }

  .tile.editing {
    outline-color: var(--muted);
    cursor: grab;
  }

  .tile.over {
    outline-color: var(--accent);
  }

  .tile.dragging {
    opacity: 0.45;
  }

  .tname {
    overflow: hidden;
    font-weight: 500;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .tsub {
    overflow: hidden;
    color: var(--muted);
    font-size: 11px;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .slot {
    display: flex;
    flex-direction: column;
    min-width: 0;
    outline: 2px dashed transparent;
    outline-offset: 4px;
  }

  .slot.editing {
    outline-color: var(--muted);
    cursor: grab;
  }

  .slot.over {
    outline-color: var(--accent);
  }

  .slot.dragging {
    opacity: 0.45;
  }

  .undo {
    left: 16px;
    right: auto;
    display: flex;
    align-items: center;
    gap: 12px;
  }

  @media (max-width: 560px) {
    .search {
      width: 100%;
    }

    .group.end {
      margin-left: 0;
      width: 100%;
    }
  }
</style>
