<script lang="ts">
  import { tr } from '../../i18n';
  import Modal from '../Modal.svelte';
  import PixIcon from '../ui/PixIcon.svelte';
  import { groupTabs, type TabId, type TABS } from './model';

  /** Phone: all sections of the server, by group, as a sheet over the page (the side menu is hidden there). */
  let {
    tabs,
    current,
    badges = {},
    onselect,
    onclose,
  }: {
    tabs: readonly (typeof TABS)[number][];
    current: TabId;
    badges?: Partial<Record<TabId, { text: string; tone: 'warn' | 'crit' }>>;
    onselect: (id: TabId) => void;
    onclose: () => void;
  } = $props();

  const groups = $derived(groupTabs(tabs));
</script>

<Modal title={$tr('nav.sections')} labelId="sections-title" {onclose} flush>
  <div class="sections">
    {#each groups as g (g.id)}
      <h3 class="grp">{$tr(g.key)}</h3>
      <div class="grid">
        {#each g.tabs as t (t.id)}
          {@const b = badges[t.id]}
          <button class="item" type="button" aria-current={current === t.id ? 'page' : undefined} onclick={() => onselect(t.id)}>
            <PixIcon name={t.id} size={20} />
            <span class="lbl">{$tr(t.key)}</span>
            {#if b}<em class="cnt {b.tone}">{b.text}</em>{/if}
          </button>
        {/each}
      </div>
    {/each}
  </div>
</Modal>

<style>
  .grp {
    margin: 0;
    padding: 10px 14px 2px;
    color: var(--muted);
    font-family: var(--font-display);
    font-size: 12.5px;
    letter-spacing: 0.08em;
    text-transform: uppercase;
  }

  .grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 8px;
    padding: 4px 12px 6px;
  }

  .item {
    position: relative;
    display: flex;
    align-items: center;
    gap: 10px;
    min-width: 0;
    height: 50px;
    padding: 0 10px;
    background: var(--surface);
    border: 2px solid var(--line);
    box-shadow: 2px 2px 0 var(--shade);
    color: var(--ink);
    cursor: pointer;
    font-family: var(--font-display);
    font-size: 15px;
    text-align: left;
  }

  .item[aria-current='page'] {
    background: var(--ink);
    color: var(--surface);
  }

  .lbl {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .cnt {
    min-width: 18px;
    height: 18px;
    padding: 0 4px;
    background: var(--warn);
    color: #fff;
    font-size: 11px;
    font-style: normal;
    line-height: 18px;
    text-align: center;
  }

  .cnt.crit {
    background: var(--crit);
  }
</style>
