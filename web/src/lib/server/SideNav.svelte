<script lang="ts">
  import { tr } from '../../i18n';
  import { groupTabs, TAB_ICONS, type TabId, type TABS } from './model';

  let {
    tabs,
    current,
    onselect,
    updates = 0,
    securityUpdates = 0,
    failedServices = 0,
    agentVersion = '',
  }: {
    tabs: readonly (typeof TABS)[number][];
    current: TabId;
    onselect: (id: TabId) => void;
    /** Counters for the badges; 0 or less hides a badge. */
    updates?: number;
    securityUpdates?: number;
    failedServices?: number;
    agentVersion?: string;
  } = $props();

  const GROUPS_KEY = 'linager.side.groups';
  const RAIL_KEY = 'linager.side.rail';

  function load<T>(key: string, fallback: T): T {
    try {
      const raw = localStorage.getItem(key);
      return raw === null ? fallback : (JSON.parse(raw) as T);
    } catch {
      return fallback;
    }
  }

  function store(key: string, value: unknown) {
    try {
      localStorage.setItem(key, JSON.stringify(value));
    } catch {
      // private mode: the state just is not kept
    }
  }

  let folded = $state<Record<string, boolean>>(load(GROUPS_KEY, {}));
  let rail = $state<boolean>(load(RAIL_KEY, false));
  let menuOpen = $state(false);

  const groups = $derived(groupTabs(tabs));
  const currentGroup = $derived(groups.find((g) => g.tabs.some((t) => t.id === current)));
  const currentTab = $derived(currentGroup?.tabs.find((t) => t.id === current));

  function toggleGroup(id: string) {
    folded = { ...folded, [id]: !folded[id] };
    store(GROUPS_KEY, folded);
  }

  function toggleRail() {
    rail = !rail;
    store(RAIL_KEY, rail);
  }

  function pick(id: TabId) {
    menuOpen = false;
    onselect(id);
  }

  function badge(id: TabId): { text: string; tone: 'warn' | 'crit'; hint: string } | null {
    if (id === 'updates' && updates > 0) {
      return {
        text: String(updates),
        tone: securityUpdates > 0 ? 'crit' : 'warn',
        hint: $tr('srv.nav.badgeUpdates', { n: updates }),
      };
    }
    if (id === 'services' && failedServices > 0) {
      return { text: String(failedServices), tone: 'crit', hint: $tr('srv.nav.badgeFailed', { n: failedServices }) };
    }
    return null;
  }

  // Up and Down move between the tabs of the whole menu.
  function onkeydown(e: KeyboardEvent) {
    if (e.key !== 'ArrowDown' && e.key !== 'ArrowUp') return;
    const nav = e.currentTarget as HTMLElement;
    const all = [...nav.querySelectorAll<HTMLElement>('[role="tab"]')];
    const i = all.indexOf(document.activeElement as HTMLElement);
    if (i < 0 || all.length === 0) return;
    e.preventDefault();
    all[(i + (e.key === 'ArrowDown' ? 1 : -1) + all.length) % all.length].focus();
  }
</script>

{#snippet icon(id: TabId)}
  <svg class="ic" viewBox="0 0 8 8" fill="currentColor" aria-hidden="true">
    {#each TAB_ICONS[id] as row, y (y)}
      {#each [...row] as cell, x (x)}
        {#if cell === '1'}<rect {x} {y} width="1" height="1" />{/if}
      {/each}
    {/each}
  </svg>
{/snippet}

<aside class="side" class:rail class:open={menuOpen}>
  <button class="menu-btn" type="button" aria-expanded={menuOpen} onclick={() => (menuOpen = !menuOpen)}>
    <span class="cur">{currentGroup ? $tr(currentGroup.key) : ''} · {currentTab ? $tr(currentTab.key) : ''}</span>
    <svg viewBox="0 0 8 8" width="12" height="12" fill="currentColor" aria-hidden="true"><path d="M0 2h8v2H6v2H4V4H2V2z" /></svg>
  </button>

  <!-- svelte-ignore a11y_no_noninteractive_element_to_interactive_role -->
  <nav class="nav" role="tablist" aria-orientation="vertical" aria-label={$tr('srv.tabsLabel')} {onkeydown}>
    {#each groups as g (g.id)}
      {@const isFolded = !!folded[g.id] && !g.tabs.some((t) => t.id === current) && !rail}
      <div class="grp">
        <button class="grp-h" type="button" aria-expanded={!isFolded} onclick={() => toggleGroup(g.id)}>
          <span>{$tr(g.key)}</span>
          <svg class="chev" class:folded={isFolded} viewBox="0 0 8 8" fill="currentColor" aria-hidden="true"><path d="M0 2h8v2H6v2H4V4H2V2z" /></svg>
        </button>
        {#if !isFolded}
          <div class="items">
            {#each g.tabs as t (t.id)}
              {@const b = badge(t.id)}
              <button
                class="item"
                type="button"
                role="tab"
                aria-selected={current === t.id}
                data-tip={$tr(t.key)}
                onclick={() => pick(t.id)}
              >
                {@render icon(t.id)}
                <span class="lbl">{$tr(t.key)}</span>
                {#if b}<span class="cnt {b.tone}" title={b.hint}>{b.text}</span>{/if}
              </button>
            {/each}
          </div>
        {/if}
      </div>
    {/each}
  </nav>

  <div class="foot">
    <span class="ver">{agentVersion}</span>
    <button
      class="collapse"
      type="button"
      title={$tr(rail ? 'srv.nav.expand' : 'srv.nav.collapse')}
      aria-label={$tr(rail ? 'srv.nav.expand' : 'srv.nav.collapse')}
      onclick={toggleRail}
    >
      <svg viewBox="0 0 8 8" fill="currentColor" aria-hidden="true"><path d="M5 0h2v2H5zM3 2h2v2H3zM1 4h2v2H1zM3 6h2v2H3zM5 6h2v2H5z" /></svg>
    </button>
  </div>
</aside>

<style>
  .side {
    position: sticky;
    top: 16px;
    width: 236px;
    flex: none;
    display: flex;
    flex-direction: column;
    max-height: calc(100vh - 32px);
    background: var(--surface);
    border: 2px solid var(--line);
    box-shadow: 4px 4px 0 var(--shade);
  }

  /* a short window scrolls the list of tabs, the footer stays */
  .nav {
    display: flex;
    flex-direction: column;
    min-height: 0;
    overflow-y: auto;
    scrollbar-width: thin;
    padding: 6px 0;
  }

  .grp {
    padding: 4px 8px 6px;
  }

  .grp + .grp {
    border-top: 2px dashed var(--seg-off);
  }

  .grp-h {
    display: flex;
    align-items: center;
    justify-content: space-between;
    width: 100%;
    height: 28px;
    margin: 2px 0;
    padding: 0 6px;
    background: none;
    border: 0;
    cursor: pointer;
    color: var(--muted);
    font-family: var(--font-display);
    font-size: 12px;
    letter-spacing: 0.1em;
    text-transform: uppercase;
    text-align: left;
  }

  .grp-h:hover {
    color: var(--ink);
  }

  .chev {
    width: 8px;
    height: 8px;
    flex: none;
    shape-rendering: crispEdges;
    transition: transform 0.12s;
  }

  .chev.folded {
    transform: rotate(-90deg);
  }

  .items {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .item {
    position: relative;
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
    height: 34px;
    padding: 0 10px;
    background: none;
    border: 2px solid transparent;
    color: var(--ink);
    cursor: pointer;
    font-size: 14px;
    text-align: left;
  }

  .item:hover {
    background: var(--seg-off);
  }

  .item:focus-visible,
  .grp-h:focus-visible,
  .collapse:focus-visible,
  .menu-btn:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
  }

  .item[aria-selected='true'] {
    background: var(--ink);
    border-color: var(--ink);
    color: var(--surface);
    box-shadow: 3px 3px 0 var(--shade);
  }

  .ic {
    width: 16px;
    height: 16px;
    flex: none;
    shape-rendering: crispEdges;
  }

  .lbl {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .cnt {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-width: 20px;
    height: 20px;
    padding: 0 5px;
    border: 2px solid currentColor;
    font-size: 12px;
  }

  .cnt.warn {
    color: var(--warn);
  }

  .cnt.crit {
    color: var(--crit);
  }

  .item[aria-selected='true'] .cnt {
    color: var(--surface);
  }

  .foot {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    padding: 8px;
    border-top: 2px solid var(--line);
  }

  .ver {
    color: var(--muted);
    font-size: 12px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .collapse {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex: none;
    width: 28px;
    height: 28px;
    padding: 0;
    background: var(--surface);
    border: 2px solid var(--line);
    box-shadow: 2px 2px 0 var(--shade);
    color: var(--ink);
    cursor: pointer;
  }

  .collapse svg {
    width: 12px;
    height: 12px;
    shape-rendering: crispEdges;
  }

  .rail .collapse svg {
    transform: rotate(180deg);
  }

  .menu-btn {
    display: none;
  }

  /* rail: icons only, wide screens */
  @media (min-width: 861px) {
    .side.rail {
      width: 60px;
    }

    .rail .nav {
      overflow: visible;
    }

    .rail .grp {
      padding: 4px 6px;
    }

    .rail .grp-h {
      height: 12px;
      justify-content: center;
      pointer-events: none;
    }

    .rail .grp-h span,
    .rail .chev {
      display: none;
    }

    .rail .grp-h::after {
      content: '';
      width: 14px;
      height: 2px;
      background: var(--line);
      opacity: 0.35;
    }

    .rail .item {
      justify-content: center;
      padding: 0;
    }

    .rail .lbl {
      display: none;
    }

    .rail .cnt {
      position: absolute;
      top: 1px;
      right: 1px;
      min-width: 0;
      width: 9px;
      height: 9px;
      padding: 0;
      border: 0;
      background: currentColor;
      font-size: 0;
    }

    .rail .foot {
      flex-direction: column;
      padding: 8px 4px;
    }

    .rail .ver {
      display: none;
    }

    .rail .item:hover::after {
      content: attr(data-tip);
      position: absolute;
      top: 50%;
      left: calc(100% + 10px);
      z-index: 5;
      transform: translateY(-50%);
      padding: 3px 8px;
      background: var(--ink);
      color: var(--surface);
      font-size: 12px;
      white-space: nowrap;
      pointer-events: none;
    }
  }

  /* Phones: the bottom bar and its "Sections" sheet replace the menu. */
  @media (max-width: 720px) {
    .side {
      display: none;
    }
  }

  /* narrow screens: the menu is a drop-down with the current section on top */
  @media (max-width: 860px) {
    .side {
      position: static;
      max-height: none;
      width: 100%;
    }

    .menu-btn {
      display: flex;
      align-items: center;
      gap: 10px;
      width: 100%;
      height: 36px;
      padding: 0 12px;
      background: var(--surface);
      border: 0;
      color: var(--ink);
      cursor: pointer;
      font-family: var(--font-display);
      font-size: 14px;
    }

    .cur {
      flex: 1;
      text-align: left;
    }

    .nav,
    .foot {
      display: none;
    }

    .side.open .nav {
      display: flex;
      border-top: 2px solid var(--line);
    }
  }
</style>
