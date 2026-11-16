<script lang="ts">
  import { untrack } from 'svelte';
  import type { Group, Server } from '../../api';
  import { tr } from '../../i18n';
  import Modal from '../Modal.svelte';

  let {
    servers,
    groups,
    selectedServers,
    selectedGroups,
    onsave,
    onclose,
  }: {
    servers: Server[];
    groups: Group[];
    selectedServers: string[];
    selectedGroups: number[];
    onsave: (servers: string[], groups: number[]) => void;
    onclose: () => void;
  } = $props();

  // edited locally, applied with "Save"
  let pickedServers = $state<string[]>(untrack(() => [...selectedServers]));
  let pickedGroups = $state<number[]>(untrack(() => [...selectedGroups]));
  let search = $state('');

  const q = $derived(search.trim().toLowerCase());
  const matches = (s: Server): boolean => !q || s.name.toLowerCase().includes(q) || s.address.toLowerCase().includes(q);

  const blocks = $derived.by(() => {
    const list = groups
      .slice()
      .sort((a, b) => a.name.localeCompare(b.name))
      .map((g) => ({ group: g as Group | null, servers: servers.filter((s) => s.group_id === g.id && matches(s)) }));
    const rest = servers.filter((s) => (s.group_id ?? null) === null && matches(s));
    if (rest.length > 0 || list.length === 0) list.push({ group: null, servers: rest });
    return list.filter((b) => b.servers.length > 0 || (b.group !== null && !q));
  });

  /** Servers the access gives: the single ones and those of the chosen groups. */
  const total = $derived(
    new Set([...pickedServers, ...servers.filter((s) => s.group_id != null && pickedGroups.includes(s.group_id)).map((s) => s.id)]).size,
  );

  const viaGroup = (s: Server): boolean => s.group_id != null && pickedGroups.includes(s.group_id);

  function toggleGroup(id: number): void {
    pickedGroups = pickedGroups.includes(id) ? pickedGroups.filter((g) => g !== id) : [...pickedGroups, id];
  }

  function toggleServer(s: Server): void {
    if (viaGroup(s)) return;
    pickedServers = pickedServers.includes(s.id) ? pickedServers.filter((x) => x !== s.id) : [...pickedServers, s.id];
  }
</script>

<Modal title={$tr('acc.servers.title')} labelId="acc-servers-title" {onclose}>
  <p class="muted small">{$tr('acc.servers.hint')}</p>
  {#if servers.length > 8}
    <input class="input" type="search" placeholder={$tr('acc.servers.search')} aria-label={$tr('acc.servers.search')} bind:value={search} />
  {/if}
  <div class="blocks">
    {#each blocks as b (b.group?.id ?? 'none')}
      <section class="block">
        {#if b.group}
          {@const g = b.group}
          <div class="head">
            <button class="sw" type="button" aria-pressed={pickedGroups.includes(g.id)} aria-label={g.name} onclick={() => toggleGroup(g.id)}></button>
            <strong>{g.name}</strong>
            <span class="muted small">{$tr('acc.servers.groupNote', { n: servers.filter((s) => s.group_id === g.id).length })}</span>
          </div>
        {:else}
          <div class="head"><strong>{$tr('acc.servers.ungrouped')}</strong></div>
        {/if}
        <ul class="list">
          {#each b.servers as s (s.id)}
            {@const locked = viaGroup(s)}
            <li class="srv" class:locked>
              <button
                class="sw"
                type="button"
                aria-pressed={locked || pickedServers.includes(s.id)}
                aria-disabled={locked}
                disabled={locked}
                aria-label={s.name}
                onclick={() => toggleServer(s)}
              ></button>
              <span class="nm">{s.name}</span>
              <span class="muted small mono">{s.address}</span>
            </li>
          {:else}
            <li class="muted small pad">{$tr('acc.servers.empty')}</li>
          {/each}
        </ul>
      </section>
    {:else}
      <p class="muted">{$tr('acc.servers.none')}</p>
    {/each}
  </div>
  <div class="row between bar">
    <span class="small muted">{$tr('acc.servers.total', { n: total })}</span>
    <div class="row">
      <button class="btn" type="button" onclick={onclose}>{$tr('common.cancel')}</button>
      <button class="btn primary" type="button" onclick={() => onsave(pickedServers, pickedGroups)}>{$tr('acc.apply')}</button>
    </div>
  </div>
</Modal>

<style>
  .blocks {
    display: flex;
    flex-direction: column;
    gap: 10px;
    max-height: min(52vh, 460px);
    overflow: auto;
  }

  .block {
    border: 2px solid var(--line);
  }

  .head {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 8px 12px;
    background: var(--seg-off);
  }

  .list {
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .srv {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 7px 12px 7px 36px;
    border-top: 1px dashed var(--seg-off);
  }

  .srv.locked {
    opacity: 0.55;
  }

  .nm {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .pad {
    padding: 8px 12px 8px 36px;
  }

  .sw {
    position: relative;
    flex: none;
    width: 38px;
    height: 22px;
    padding: 0;
    border: 2px solid var(--line);
    background: var(--seg-off);
    cursor: pointer;
  }

  .sw::after {
    content: '';
    position: absolute;
    top: 2px;
    left: 2px;
    width: 14px;
    height: 14px;
    background: var(--ink);
    transition: transform 0.15s;
  }

  .sw[aria-pressed='true'] {
    background: var(--good);
  }

  .sw[aria-pressed='true']::after {
    transform: translateX(16px);
    background: var(--surface);
  }

  /* a server given by its group: on, but grey and not editable */
  .sw:disabled {
    background: var(--muted);
    cursor: not-allowed;
  }

  .sw:disabled[aria-pressed='true']::after {
    transform: translateX(16px);
    background: var(--surface);
  }

  p {
    margin: 0;
  }

  /* Phones: the sheet scrolls as a whole, the switches get a bigger tap area, the buttons stay at the bottom. */
  @media (max-width: 720px) {
    .blocks,
    .list {
      max-height: none;
      overflow: visible;
    }

    .sw::before {
      content: '';
      position: absolute;
      inset: -11px -6px;
    }

    .bar {
      position: sticky;
      bottom: 0;
      z-index: 2;
      flex-direction: column;
      align-items: stretch;
      gap: 8px;
      margin: 0 -12px calc(-12px - var(--safe-b));
      padding: 10px 12px calc(10px + var(--safe-b));
      background: var(--surface);
      border-top: 2px solid var(--line);
    }

    .bar > .row {
      flex-wrap: nowrap;
    }

    .bar > .row > .btn {
      flex: 1;
    }

    .srv {
      min-height: 48px;
    }
  }
</style>
