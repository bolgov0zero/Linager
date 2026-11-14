<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import { api, isUnauthorized, type LiveEvent, type ServerCard } from '../api';
  import { locale, t, tr } from '../i18n';
  import { subscribeLive } from '../live';
  import CardView from './dashboard/CardView.svelte';
  import { pluralForm } from './dashboard/format';
  import RowView from './dashboard/RowView.svelte';
  import Summary from './dashboard/Summary.svelte';
  import { levelOf, summarize } from './dashboard/status';
  import { showToast } from './toast';

  type Filter = 'all' | 'problems' | 'offline';
  type View = 'cards' | 'rows';

  let {
    editing,
    reloadKey,
    onopen,
    onauthlost,
  }: {
    editing: boolean;
    /** Bumped by the parent after the server list changed (add or pair). */
    reloadKey: number;
    onopen: (name: string) => void;
    onauthlost: () => void;
  } = $props();

  const VIEW_KEY = 'linager.dashboard.view';
  const LIVE_RELOAD_MS = 500;
  const SAFETY_RELOAD_MS = 60_000;
  const TICK_MS = 15_000;
  const ORDER_SAVE_MS = 400;
  const UNDO_MS = 5_000;

  function readView(): View {
    try {
      const value = localStorage.getItem(VIEW_KEY);
      if (value === 'cards' || value === 'rows') return value;
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

  let items = $state<ServerCard[]>([]);
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

  let seenReload = -1;
  let reloadTimer: ReturnType<typeof setTimeout> | undefined;
  let orderTimer: ReturnType<typeof setTimeout> | undefined;
  let undoTimer: ReturnType<typeof setTimeout> | undefined;

  const totals = $derived(summarize(items));
  const countLabel = $derived($tr(`dash.servers.${pluralForm($locale, items.length)}`, { n: items.length }));
  const visible = $derived.by(() => {
    const q = query.trim().toLowerCase();
    return items.filter((card) => {
      if (filter === 'offline' && card.online) return false;
      if (filter === 'problems' && levelOf(card) === 'ok') return false;
      if (!q) return true;
      return card.name.toLowerCase().includes(q) || card.address.toLowerCase().includes(q);
    });
  });

  function indexOf(id: string): number {
    return items.findIndex((c) => c.id === id);
  }

  async function load(): Promise<void> {
    try {
      const res = await api.dashboard();
      const hidden = pending?.card.id;
      items = hidden ? res.items.filter((c) => c.id !== hidden) : res.items;
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

  function move(id: string, dir: -1 | 1): void {
    const index = indexOf(id);
    const target = index + dir;
    if (index < 0 || target < 0 || target >= items.length) return;
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
      </div>
      <div class="group end">
        <button class="chip" type="button" aria-pressed={view === 'cards'} onclick={() => setView('cards')}
          >{$tr('dash.view.cards')}</button>
        <button class="chip" type="button" aria-pressed={view === 'rows'} onclick={() => setView('rows')}
          >{$tr('dash.view.rows')}</button>
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
              {now}
              {editing}
              index={indexOf(card.id)}
              count={items.length}
              {onopen}
              onmove={(dir) => move(card.id, dir)}
              onremove={() => void removeCard(card)} />
          </div>
        {/each}
      </div>
    {:else}
      <div class="list">
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
            <RowView
              {card}
              {now}
              {editing}
              index={indexOf(card.id)}
              count={items.length}
              {onopen}
              onmove={(dir) => move(card.id, dir)}
              onremove={() => void removeCard(card)} />
          </div>
        {/each}
      </div>
    {/if}
  {/if}
</section>

{#if pending}
  <div class="toast undo" role="status" aria-live="polite">
    <span>{$tr('dash.undo.text', { name: pending.card.name })}</span>
    <button class="btn" type="button" onclick={undoRemoval}>{$tr('dash.undo.button')}</button>
  </div>
{/if}

<style>
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

  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(min(300px, 100%), 1fr));
    gap: 16px;
  }

  .list {
    display: flex;
    flex-direction: column;
    gap: 12px;
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
