<script lang="ts">
  import { tr } from '../../i18n';
  import TermIcon from '../terminal/TermIcon.svelte';
  import { groupTabs, TAB_ICONS, type TabId, type TABS } from './model';

  let {
    tabs,
    current,
    onselect,
    updates = 0,
    securityUpdates = 0,
    failedServices = 0,
    agentVersion = '',
    onterminalwindow,
    ondesktopwindow,
  }: {
    tabs: readonly (typeof TABS)[number][];
    current: TabId;
    onselect: (id: TabId) => void;
    /** Counters for the badges; 0 or less hides a badge. */
    updates?: number;
    securityUpdates?: number;
    failedServices?: number;
    agentVersion?: string;
    /** Opens the terminal of the server in a floating window; the button sits next to the Terminal item. */
    onterminalwindow?: () => void;
    /** Opens the remote desktop in a floating window; the button sits next to the Desktop item (Windows servers). */
    ondesktopwindow?: () => void;
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

  // The menu stays in place while the page scrolls: it sticks right under the pinned top bar and is never taller than
  // the free part of the screen at the top of the page, so it does not make the page scroll by itself; a long list of
  // tabs scrolls inside the menu. On narrow screens the menu is a plain dropdown and none of this applies.
  const PAGE_GAP = 16;
  const PAGE_BOTTOM = 28;
  const MIN_HEIGHT = 240;

  let side = $state<HTMLElement | null>(null);

  $effect(() => {
    const el = side;
    const row = el?.parentElement;
    if (!el || !row) return;
    const wide = window.matchMedia('(min-width: 861px)');

    const measure = () => {
      if (!wide.matches) {
        el.style.removeProperty('--side-top');
        el.style.removeProperty('--side-max');
        return;
      }
      const bar = document.querySelector<HTMLElement>('.top');
      const stick = (bar?.offsetHeight ?? 0) + PAGE_GAP;
      const rowTop = row.getBoundingClientRect().top + window.scrollY;
      el.style.setProperty('--side-top', `${stick}px`);
      el.style.setProperty('--side-max', `${Math.max(MIN_HEIGHT, Math.floor(window.innerHeight - rowTop - PAGE_BOTTOM))}px`);
    };

    measure();
    const watch = new ResizeObserver(measure);
    watch.observe(document.body);
    const bar = document.querySelector('.top');
    if (bar) watch.observe(bar);
    if (row.parentElement) watch.observe(row.parentElement);
    window.addEventListener('resize', measure);
    wide.addEventListener('change', measure);
    return () => {
      watch.disconnect();
      window.removeEventListener('resize', measure);
      wide.removeEventListener('change', measure);
    };
  });

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
  <svg class="ic" viewBox="0 0 9 9" fill="currentColor" aria-hidden="true">
    {#each TAB_ICONS[id] as row, y (y)}
      {#each [...row] as cell, x (x)}
        {#if cell === '1'}<rect {x} {y} width="1" height="1" />{/if}
      {/each}
    {/each}
  </svg>
{/snippet}

<aside class="side" class:rail class:open={menuOpen} bind:this={side}>
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
              <div class="entry">
                <button
                  class="item"
                  class:withwin={(t.id === 'terminal' && !!onterminalwindow) || (t.id === 'desktop' && !!ondesktopwindow)}
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
                {#if t.id === 'terminal' && onterminalwindow}
                  <button
                    class="winbtn"
                    class:on={current === t.id}
                    type="button"
                    title={$tr('termwin.openWindow')}
                    aria-label={$tr('termwin.openWindow')}
                    onclick={onterminalwindow}><TermIcon name="window" /></button>
                {/if}
                {#if t.id === 'desktop' && ondesktopwindow}
                  <button
                    class="winbtn"
                    class:on={current === t.id}
                    type="button"
                    title={$tr('termwin.openWindow')}
                    aria-label={$tr('termwin.openWindow')}
                    onclick={ondesktopwindow}><TermIcon name="window" /></button>
                {/if}
              </div>
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
      <svg viewBox="1 0 7 7" fill="currentColor" aria-hidden="true"><path d="M5 0h2v1H5zM4 1h2v1H4zM3 2h2v1H3zM2 3h2v1H2zM3 4h2v1H3zM4 5h2v1H4zM5 6h2v1H5z" /></svg>
    </button>
  </div>
</aside>

<style>
  .side {
    position: sticky;
    top: var(--side-top, 14px);
    width: 212px;
    flex: none;
    display: flex;
    flex-direction: column;
    max-height: var(--side-max, calc(100vh - 29px));
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
    padding: 5px 0;
  }

  .grp {
    padding: 4px 7px 5px;
  }

  .grp + .grp {
    border-top: 2px dashed var(--seg-off);
  }

  .grp-h {
    display: flex;
    align-items: center;
    justify-content: space-between;
    width: 100%;
    height: 25px;
    margin: 2px 0;
    padding: 0 5px;
    background: none;
    border: 0;
    cursor: pointer;
    color: var(--muted);
    font-family: var(--font-display);
    font-size: 11px;
    letter-spacing: 0.1em;
    text-transform: uppercase;
    text-align: left;
  }

  .grp-h:hover {
    color: var(--ink);
  }

  .chev {
    width: 7px;
    height: 7px;
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

  .entry {
    position: relative;
  }

  .item.withwin {
    padding-right: 36px;
  }

  /* The "open in a window" button at the right end of the Terminal item. */
  .winbtn {
    position: absolute;
    top: 50%;
    right: 5px;
    z-index: 1;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 23px;
    height: 23px;
    padding: 0;
    transform: translateY(-50%);
    background: var(--surface);
    border: 2px solid var(--line);
    color: var(--ink);
    cursor: pointer;
  }

  .winbtn:hover {
    background: var(--seg-off);
  }

  .winbtn:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }

  .winbtn :global(svg) {
    width: 13px;
    height: 13px;
  }

  .rail .winbtn {
    display: none;
  }

  .rail .item.withwin {
    padding-right: 0;
  }

  .item {
    position: relative;
    display: flex;
    align-items: center;
    gap: 9px;
    width: 100%;
    height: 31px;
    padding: 0 9px;
    background: none;
    border: 2px solid transparent;
    color: var(--ink);
    cursor: pointer;
    font-size: 13px;
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
    min-width: 18px;
    height: 18px;
    padding: 0 5px;
    border: 2px solid currentColor;
    font-size: 11px;
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
    gap: 7px;
    padding: 7px;
    border-top: 2px solid var(--line);
  }

  .ver {
    color: var(--muted);
    font-size: 11px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .collapse {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex: none;
    width: 25px;
    height: 25px;
    padding: 0;
    background: var(--surface);
    border: 2px solid var(--line);
    box-shadow: 2px 2px 0 var(--shade);
    color: var(--ink);
    cursor: pointer;
  }

  .collapse svg {
    width: 13px;
    height: 13px;
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
      width: 54px;
    }

    .rail .nav {
      overflow: visible;
    }

    .rail .grp {
      padding: 4px 5px;
    }

    .rail .grp-h {
      height: 11px;
      justify-content: center;
      pointer-events: none;
    }

    .rail .grp-h span,
    .rail .chev {
      display: none;
    }

    .rail .grp-h::after {
      content: '';
      width: 13px;
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
      width: 8px;
      height: 8px;
      padding: 0;
      border: 0;
      background: currentColor;
      font-size: 0;
    }

    .rail .foot {
      flex-direction: column;
      padding: 7px 4px;
    }

    .rail .ver {
      display: none;
    }

    .rail .item:hover::after {
      content: attr(data-tip);
      position: absolute;
      top: 50%;
      left: calc(100% + 9px);
      z-index: 5;
      transform: translateY(-50%);
      padding: 3px 7px;
      background: var(--ink);
      color: var(--surface);
      font-size: 11px;
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
      gap: 9px;
      width: 100%;
      height: 32px;
      padding: 0 11px;
      background: var(--surface);
      border: 0;
      color: var(--ink);
      cursor: pointer;
      font-family: var(--font-display);
      font-size: 13px;
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
