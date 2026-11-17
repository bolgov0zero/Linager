<script lang="ts">
  import { onDestroy, untrack } from 'svelte';
  import type { Group, SchemeLayout, ServerCard } from '../../api';
  import { tr } from '../../i18n';
  import { openTermWindow } from '../terminal/windows';
  import { fmtPct, fmtRate } from './format';
  import {
    PANEL_H,
    PANEL_W,
    LINK_STYLES,
    ROUTE_MAX,
    autoLayout,
    busPath,
    clamp,
    fitView,
    routeLinks,
    snap,
    speedLevel,
    zoomAround,
    type Box,
    type LinkStyle,
    type NodeMode,
    type Pt,
    type View,
  } from './schemeLayout';
  import { levelOf, toneOfPct, type Level } from './status';

  let {
    cards,
    total,
    online,
    layout,
    groups,
    onopen,
    onlayout,
    onreset,
    onadd,
  }: {
    /** Servers to draw; the filters, the group and the search are already applied. */
    cards: ServerCard[];
    /** All servers of the dashboard, for "online N of M". */
    total: number;
    /** Online servers among all of them. */
    online: number;
    /** Positions saved on the server; null = automatic layout. Used as the initial value. */
    layout: SchemeLayout | null;
    groups: Group[];
    onopen: (name: string, tab?: string) => void;
    /** Called once when a drag ends, with every manual position. */
    onlayout: (layout: SchemeLayout) => void;
    onreset: () => void;
    onadd?: () => void;
  } = $props();

  /** A node placed on the plane (or in the phone column): centre and size in px. */
  interface Row {
    card: ServerCard;
    i: number;
    lvl: Level;
    text: string;
    cx: number;
    cy: number;
    w: number;
    h: number;
  }
  interface Frame {
    id: number;
    name: string;
    left: number;
    top: number;
    w: number;
    h: number;
  }
  interface Head {
    top: number;
    text: string;
  }
  interface Drag {
    kind: 'node' | 'panel' | 'group' | 'pan';
    key: string;
    sx: number;
    sy: number;
    ox: number;
    oy: number;
    moved: boolean;
    nodrag: boolean;
    start: Record<string, Pt>;
    tx0: number;
    ty0: number;
  }

  const LINKS_KEY = 'linager.dashboard.scheme.links';
  const PHONE_MAX = 720;
  const TRUNK_X = 26;
  const STUB_X = 50;

  let rootW = $state(0);
  let stageW = $state(0);
  let stageEl = $state<HTMLDivElement | null>(null);

  let sel = $state<string | null>(null);
  let view = $state.raw<View>({ k: 1, tx: 0, ty: 0 });
  /** True once the user has zoomed or panned: the view is then no longer fitted automatically. */
  let userView = false;
  let fitTick = $state(0);
  // Manual positions (by server id). null = the automatic layout. The saved layout is only the initial value.
  let manual = $state.raw<Record<string, Pt> | null>(untrack(() => (layout ? { ...layout.nodes } : null)));
  let manualPanel = $state.raw<Pt | null>(untrack(() => (layout ? (layout.panel ?? { x: 0, y: 0 }) : null)));
  let hadLayout = untrack(() => layout !== null);
  let drag: Drag | null = null;
  let dragEnd = 0;
  /** True while a server, the panel or a group is being dragged: the router waits for the drop. */
  let dragging = $state(false);

  function readLinks(): LinkStyle {
    try {
      const v = localStorage.getItem(LINKS_KEY);
      if (v === 'bus' || v === 'route') return v;
    } catch {
      // storage unavailable: the default style
    }
    return 'straight';
  }
  let linkStyle = $state<LinkStyle>(readLinks());
  function setLinks(next: LinkStyle): void {
    linkStyle = next;
    try {
      localStorage.setItem(LINKS_KEY, next);
    } catch {
      // storage unavailable: the choice just is not remembered
    }
  }

  const vertical = $derived(rootW > 0 && rootW < PHONE_MAX);

  // Groups stand together: the order is by group id, servers without a group last.
  const sorted = $derived(
    [...cards].sort((a, b) => (a.group_id ?? Number.MAX_SAFE_INTEGER) - (b.group_id ?? Number.MAX_SAFE_INTEGER)),
  );
  const count = $derived(sorted.length);
  // Depends on the number of nodes only, so that a metrics tick never moves anything.
  const autoL = $derived(autoLayout(count, linkStyle === 'route'));
  const auto = $derived.by(() => {
    const m: Record<string, Pt> = {};
    sorted.forEach((c, i) => {
      m[c.id] = autoL.pos[i] ?? { x: 0, y: 0 };
    });
    return m;
  });

  /** The place of every drawn server: manual where known, newcomers in a column to the right of everything. */
  const pos = $derived.by((): Record<string, Pt> => {
    const man = manual;
    if (!man) return auto;
    const out: Record<string, Pt> = {};
    const fresh: ServerCard[] = [];
    let maxX = -Infinity;
    let minY = Infinity;
    for (const c of sorted) {
      const p = man[c.id];
      if (p) {
        out[c.id] = p;
        maxX = Math.max(maxX, p.x + autoL.nw / 2);
        minY = Math.min(minY, p.y);
      } else {
        fresh.push(c);
      }
    }
    if (fresh.length > 0) {
      if (!Number.isFinite(maxX)) {
        maxX = PANEL_W / 2;
        minY = -PANEL_H / 2;
      }
      fresh.forEach((c, i) => {
        out[c.id] = { x: snap(maxX + autoL.nw / 2 + 50), y: snap(minY + i * (autoL.nh + 18)) };
      });
    }
    return out;
  });
  const panelPos = $derived<Pt>(manualPanel ?? { x: 0, y: 0 });
  const isManual = $derived(manual !== null);

  const bbox = $derived.by((): Box => {
    const pp = panelPos;
    const b: Box = [pp.x - PANEL_W / 2, pp.y - PANEL_H / 2, pp.x + PANEL_W / 2, pp.y + PANEL_H / 2];
    for (const c of sorted) {
      const q = pos[c.id];
      if (!q) continue;
      b[0] = Math.min(b[0], q.x - autoL.nw / 2);
      b[1] = Math.min(b[1], q.y - autoL.nh / 2);
      b[2] = Math.max(b[2], q.x + autoL.nw / 2);
      b[3] = Math.max(b[3], q.y + autoL.nh / 2);
    }
    return b;
  });

  function groupName(id: number): string {
    return groups.find((g) => g.id === id)?.name ?? `#${id}`;
  }

  /** The phone column: the panel on top, a trunk on the left, a stub to every server. */
  const vlay = $derived.by(() => {
    if (!vertical || stageW <= 0 || count === 0) return null;
    const compact = count > 12;
    const nw = stageW - STUB_X - 12;
    const nh = compact ? 34 : 58;
    const panelTop = 12;
    const panelH = 66;
    const top = panelTop + panelH;
    let cursor = top + 22;
    let lastY = cursor;
    const ys: number[] = [];
    const heads: Head[] = [];
    const showGroups = sorted.some((c) => c.group_id !== null);
    let prev: number | null | undefined;
    for (const c of sorted) {
      if (showGroups && c.group_id !== prev) {
        heads.push({ top: cursor, text: c.group_id === null ? $tr('dash.group.none') : groupName(c.group_id) });
        cursor += 22;
        prev = c.group_id;
      }
      const yc = cursor + nh / 2;
      ys.push(yc);
      lastY = yc;
      cursor += nh + 12;
    }
    return {
      nw,
      nh,
      mode: (compact ? 'compact' : 'full') as NodeMode,
      ys,
      heads,
      top,
      lastY,
      panelW: stageW - 24,
      panelH,
      panelCy: panelTop + panelH / 2,
      H: cursor + 6,
    };
  });

  const mode = $derived<NodeMode>(vlay ? vlay.mode : autoL.mode);

  /** Why a server is not green, derived the way levelOf decides; short enough for a node. */
  function statusText(card: ServerCard, lvl: Level): string {
    if (!card.online) return $tr('dash.offline');
    const s = card.sample;
    if (lvl === 'ok' || !s) return $tr('scheme.st.online');
    if (lvl === 'crit') {
      if (s.cpu > 90) return $tr('scheme.st.cpu', { v: fmtPct(s.cpu) });
      if (s.mem_pct > 92) return $tr('scheme.st.ram', { v: fmtPct(s.mem_pct) });
      return $tr('scheme.st.disk', { v: fmtPct(s.disk_pct) });
    }
    if (s.cpu > 75) return $tr('scheme.st.cpu', { v: fmtPct(s.cpu) });
    if (s.mem_pct > 85) return $tr('scheme.st.ram', { v: fmtPct(s.mem_pct) });
    if (s.disk_pct > 80) return $tr('scheme.st.disk', { v: fmtPct(s.disk_pct) });
    if (s.failed_services > 0) return $tr('scheme.st.failed', { n: s.failed_services });
    if (s.reboot_required) return $tr('scheme.st.reboot');
    return $tr('scheme.st.updates', { n: s.updates });
  }

  const rows = $derived.by((): Row[] => {
    const v = vlay;
    return sorted.map((card, i) => {
      const lvl = levelOf(card);
      const base = { card, i, lvl, text: statusText(card, lvl) };
      if (v) return { ...base, cx: STUB_X + v.nw / 2, cy: v.ys[i] ?? 0, w: v.nw, h: v.nh };
      const p = pos[card.id] ?? { x: 0, y: 0 };
      return { ...base, cx: p.x, cy: p.y, w: autoL.nw, h: autoL.nh };
    });
  });

  /** Group frames (the roomy mode of the plane only): the box around the nodes of a group. */
  const frames = $derived.by((): Frame[] => {
    if (vertical || autoL.mode !== 'full') return [];
    const gb = new Map<number, Box>();
    for (const c of sorted) {
      if (c.group_id === null) continue;
      const q = pos[c.id];
      if (!q) continue;
      const g = gb.get(c.group_id) ?? [Infinity, Infinity, -Infinity, -Infinity];
      g[0] = Math.min(g[0], q.x - autoL.nw / 2);
      g[1] = Math.min(g[1], q.y - autoL.nh / 2);
      g[2] = Math.max(g[2], q.x + autoL.nw / 2);
      g[3] = Math.max(g[3], q.y + autoL.nh / 2);
      gb.set(c.group_id, g);
    }
    return [...gb].map(([id, g]) => ({
      id,
      name: groupName(id),
      left: g[0] - 10,
      top: g[1] - 12,
      w: g[2] - g[0] + 20,
      h: g[3] - g[1] + 22,
    }));
  });

  // A route is the node and its parents up to the panel. Today an agent hangs on the panel; later a device will hang
  // on its agent and the same function will light the whole way.
  function route(card: ServerCard): string[] {
    return [card.id];
  }
  const selCard = $derived(sel ? (cards.find((c) => c.id === sel) ?? null) : null);
  const litSet = $derived(selCard ? new Set(route(selCard)) : null);
  function lit(id: string): boolean {
    return litSet === null || litSet.has(id);
  }
  /** Grey links first, the lit route on top. */
  const linkRows = $derived([...rows].sort((a, b) => Number(lit(a.card.id)) - Number(lit(b.card.id))));

  function straightPath(a: Pt, b: Pt): string {
    return `M ${a.x} ${a.y} L ${b.x} ${b.y}`;
  }
  /** The shape of a link on the plane (the phone column always uses straight pieces). */
  function linkPath(a: Pt, b: Pt, i: number): string {
    return linkStyle === 'straight' ? straightPath(a, b) : busPath(a, b, i);
  }
  // The router looks at the nodes and the links already laid; it runs when the positions change, not on a click or a
  // metrics tick, and waits for the drop while something is dragged. A link it cannot route gets the tracks.
  const routed = $derived.by((): Record<string, string | null> | null => {
    if (linkStyle !== 'route' || dragging || !showPlane || count === 0 || count > ROUTE_MAX) return null;
    const nodes = sorted.map((c) => ({ id: c.id, ...(pos[c.id] ?? { x: 0, y: 0 }) }));
    return routeLinks(nodes, autoL.nw, autoL.nh, panelPos, bbox);
  });
  function linkClass(r: Row): string {
    if (!lit(r.card.id)) return 'lk gray';
    const rate = r.card.sample ? r.card.sample.rx_bps + r.card.sample.tx_bps : 0;
    return `lk ${r.lvl}${r.lvl === 'off' ? '' : ` run s${speedLevel(rate)}`}${selCard ? ' hot' : ''}`;
  }

  const stageH = $derived.by(() => {
    if (count === 0) return total === 0 ? 360 : 240;
    if (vlay) return vlay.H;
    return clamp(Math.round(stageW * 0.5), 440, 660);
  });
  const showPlane = $derived(count > 0 && !vertical);
  const worldTransform = $derived(showPlane ? `translate(${view.tx}px, ${view.ty}px) scale(${view.k})` : 'none');
  const pct = $derived(Math.round(view.k * 100));

  // Selection
  function select(id: string): void {
    sel = sel === id ? null : id;
  }

  // View: fit while the user has not changed it
  function refit(): void {
    userView = false;
    fitTick++;
  }
  $effect(() => {
    void count;
    void fitTick;
    const w = stageW;
    const h = stageH;
    if (!showPlane || w <= 0 || userView) return;
    view = fitView(
      untrack(() => bbox),
      w,
      h,
    );
  });

  function zoomAt(px: number, py: number, f: number): void {
    view = zoomAround(view, px, py, f);
    userView = true;
  }
  function zoomCentre(f: number): void {
    zoomAt(stageW / 2, stageH / 2, f);
  }
  function fitNow(): void {
    refit();
  }

  // Ctrl or Cmd with the wheel zooms around the cursor; the plain wheel still scrolls the page.
  $effect(() => {
    const el = stageEl;
    if (!el) return;
    const onWheel = (e: WheelEvent) => {
      if (!(e.ctrlKey || e.metaKey) || !showPlane) return;
      e.preventDefault();
      const r = el.getBoundingClientRect();
      zoomAt(e.clientX - r.left, e.clientY - r.top, Math.exp(-e.deltaY * (e.ctrlKey ? 0.01 : 0.0015)));
    };
    el.addEventListener('wheel', onWheel, { passive: false });
    return () => el.removeEventListener('wheel', onWheel);
  });

  // The saved layout: adopted when it arrives and nothing is placed yet, dropped when it becomes null (a reset).
  $effect(() => {
    const l = layout;
    untrack(() => {
      if (l === null) {
        if (hadLayout) {
          manual = null;
          manualPanel = null;
          refit();
        }
      } else if (manual === null) {
        manual = { ...l.nodes };
        manualPanel = l.panel ?? { x: 0, y: 0 };
        refit();
      }
      hadLayout = l !== null;
    });
  });

  function resetLayout(): void {
    manual = null;
    manualPanel = null;
    refit();
    onreset();
  }

  // Dragging: a server, the panel, a whole group by its label, or the background (pans the view).
  // A move of less than 4 px is a click.
  function begin(e: PointerEvent, kind: Drag['kind'], key: string, ox: number, oy: number): void {
    if (e.button !== 0) return;
    const start: Record<string, Pt> = {};
    if (kind === 'group') {
      for (const c of sorted) {
        const p = pos[c.id];
        if (String(c.group_id) === key && p) start[c.id] = p;
      }
    }
    drag = { kind, key, sx: e.clientX, sy: e.clientY, ox, oy, moved: false, nodrag: vertical, start, tx0: view.tx, ty0: view.ty };
  }

  function toManual(): void {
    if (manual) return;
    const m: Record<string, Pt> = {};
    for (const c of sorted) m[c.id] = pos[c.id] ?? { x: 0, y: 0 };
    manual = m;
    manualPanel = { x: 0, y: 0 };
  }

  function stagePointerDown(e: PointerEvent): void {
    if (e.button !== 0 || !showPlane) return;
    if ((e.target as Element).closest('.n, .pn, .gf span, .zoom')) return;
    begin(e, 'pan', '', 0, 0);
  }

  function onMove(e: PointerEvent): void {
    const d = drag;
    if (!d || d.nodrag) return;
    const dx = e.clientX - d.sx;
    const dy = e.clientY - d.sy;
    if (!d.moved) {
      if (Math.hypot(dx, dy) < 4) return;
      d.moved = true;
      if (d.kind !== 'pan') {
        toManual();
        dragging = true;
      }
      document.body.classList.add('scheme-drag');
    }
    if (d.kind === 'pan') {
      view = { k: view.k, tx: d.tx0 + dx, ty: d.ty0 + dy };
      userView = true;
      return;
    }
    const wx = dx / view.k;
    const wy = dy / view.k;
    if (d.kind === 'node') {
      manual = { ...(manual ?? {}), [d.key]: { x: snap(d.ox + wx), y: snap(d.oy + wy) } };
    } else if (d.kind === 'panel') {
      manualPanel = { x: snap(d.ox + wx), y: snap(d.oy + wy) };
    } else {
      const next: Record<string, Pt> = { ...(manual ?? {}) };
      for (const [id, p] of Object.entries(d.start)) next[id] = { x: snap(p.x + wx), y: snap(p.y + wy) };
      manual = next;
    }
  }

  function emitLayout(): void {
    const nodes: Record<string, Pt> = { ...(manual ?? {}) };
    for (const c of sorted) {
      const p = pos[c.id];
      if (p) nodes[c.id] = { x: Math.round(p.x), y: Math.round(p.y) };
    }
    const pp = panelPos;
    manual = nodes;
    const panel = { x: Math.round(pp.x), y: Math.round(pp.y) };
    manualPanel = panel;
    onlayout({ panel, nodes });
  }

  function onUp(): void {
    const d = drag;
    if (!d) return;
    drag = null;
    dragging = false;
    document.body.classList.remove('scheme-drag');
    if (d.moved) {
      dragEnd = Date.now();
      if (d.kind !== 'pan') emitLayout();
      return;
    }
    if (d.kind === 'node') select(d.key);
  }

  function onCancel(): void {
    drag = null;
    dragging = false;
    document.body.classList.remove('scheme-drag');
  }

  onDestroy(() => {
    document.body.classList.remove('scheme-drag');
  });

  function stageClick(e: MouseEvent): void {
    if (Date.now() - dragEnd < 80) return;
    if (sel && !(e.target as Element).closest('.n, .pn, .gf, .gh, .zoom')) sel = null;
  }

  function nodeTitle(r: Row): string {
    return `${r.card.name} · ${r.card.address} · ${r.text}`;
  }
</script>

<svelte:document onpointermove={onMove} onpointerup={onUp} onpointercancel={onCancel} />

{#snippet node(r: Row)}
  <button
    type="button"
    class="n {mode === 'full' ? 'full' : 'comp'} st-{r.lvl}"
    class:sel={sel === r.card.id}
    class:gray={!lit(r.card.id)}
    style:left="{Math.round(r.cx - r.w / 2)}px"
    style:top="{Math.round(r.cy - r.h / 2)}px"
    style:width="{r.w}px"
    style:height="{r.h}px"
    title={nodeTitle(r)}
    onpointerdown={(e) => begin(e, 'node', r.card.id, r.cx, r.cy)}
    onclick={(e) => {
      if (e.detail === 0) select(r.card.id);
    }}
  >
    {#if mode === 'full'}
      <b class="nm">{r.card.name}</b>
      <span class="stt"><i class="sq"></i>{r.text}</span>
    {:else}
      <i class="sq"></i><b class="nm">{r.card.name}</b>
    {/if}
  </button>
{/snippet}

{#snippet metric(label: string, v: number | null)}
  <span>{label} <b class={toneOfPct(v)}>{fmtPct(v)}</b></span>
{/snippet}

<section class="card scheme" bind:clientWidth={rootW}>
  <header>
    <h3>{$tr('scheme.title')}</h3>
    <span class="meta">
      {#if total > 0}{$tr('scheme.online', { n: online, m: total })}{#if count !== total} · {$tr('scheme.shown', { n: count })}{/if}{/if}
    </span>
    {#if showPlane}
      <div class="lsw" role="group" aria-label={$tr('scheme.links.label')}>
        <span class="lbl">{$tr('scheme.links.label')}</span>
        {#each LINK_STYLES as st (st)}
          <button type="button" class="chip" aria-pressed={linkStyle === st} title={$tr(`scheme.links.${st}.title`)} onclick={() => setLinks(st)}
            >{$tr(`scheme.links.${st}`)}</button>
        {/each}
        {#if linkStyle === 'route' && count > ROUTE_MAX}
          <span class="lbl warn" role="status">{$tr('scheme.links.routeLimit', { n: ROUTE_MAX })}</span>
        {/if}
      </div>
    {/if}
    {#if isManual && showPlane}
      <button type="button" class="btn sm" onclick={resetLayout}>{$tr('scheme.reset')}</button>
    {/if}
  </header>

  <!-- svelte-ignore a11y_no_static_element_interactions, a11y_click_events_have_key_events -->
  <div
    class="stage"
    class:mv={showPlane}
    bind:this={stageEl}
    bind:clientWidth={stageW}
    style:height="{stageH}px"
    style:background-size={showPlane ? `${20 * view.k}px ${20 * view.k}px` : undefined}
    style:background-position={showPlane ? `${view.tx}px ${view.ty}px` : undefined}
    aria-label={$tr('scheme.plane')}
    onpointerdown={stagePointerDown}
    onclick={stageClick}
  >
    {#if count === 0}
      <div class="void" class:none={total === 0}>
        <div class="pn static">
          <div class="t"><i>&gt;_</i> Linager</div>
          <div class="s">{$tr('scheme.panel')}{#if total > 0} · {$tr('scheme.online', { n: online, m: total })}{/if}</div>
        </div>
        {#if total === 0}
          <span class="vline"></span>
          <div class="ph">
            <span>{$tr('scheme.empty')}</span>
            {#if onadd}<button type="button" class="btn sm primary" onclick={() => onadd?.()}>{$tr('servers.add')}</button>{/if}
          </div>
        {:else}
          <p class="note">{$tr('scheme.noMatch')}</p>
        {/if}
      </div>
    {:else if vlay}
      <div class="world">
        <svg class="lines" aria-hidden="true">
          <path class="lk ok run s2" class:gray={selCard !== null} d={straightPath({ x: TRUNK_X, y: vlay.lastY }, { x: TRUNK_X, y: vlay.top })} />
          {#each linkRows as r (r.card.id)}
            <path class={linkClass(r)} d={straightPath({ x: STUB_X, y: r.cy }, { x: TRUNK_X, y: r.cy })} />
          {/each}
          {#each rows as r (r.card.id)}
            {#if selCard && selCard.id === r.card.id}
              <path class={linkClass(r)} d={straightPath({ x: TRUNK_X, y: r.cy }, { x: TRUNK_X, y: vlay.top })} />
            {/if}
          {/each}
        </svg>
        <div class="pn" style:left="12px" style:top="{vlay.panelCy - vlay.panelH / 2}px" style:width="{vlay.panelW}px" style:height="{vlay.panelH}px">
          <div class="t"><i>&gt;_</i> Linager</div>
          <div class="s">{$tr('scheme.panel')} · {$tr('scheme.online', { n: online, m: total })}</div>
        </div>
        {#each vlay.heads as h (h.top)}
          <div class="gh" style:left="{STUB_X}px" style:top="{h.top}px">{h.text}</div>
        {/each}
        {#each rows as r (r.card.id)}
          {@render node(r)}
        {/each}
      </div>
    {:else}
      <div class="world" style:transform={worldTransform}>
        <svg class="lines" aria-hidden="true">
          {#each linkRows as r (r.card.id)}
            <path class={linkClass(r)} d={routed?.[r.card.id] ?? linkPath({ x: r.cx, y: r.cy }, panelPos, r.i)} />
          {/each}
        </svg>
        {#each frames as f (f.id)}
          <div class="gf" class:gray={selCard !== null && selCard.group_id !== f.id} style:left="{f.left}px" style:top="{f.top}px" style:width="{f.w}px" style:height="{f.h}px">
            <span role="presentation" onpointerdown={(e) => begin(e, 'group', String(f.id), 0, 0)}>{f.name}</span>
          </div>
        {/each}
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div
          class="pn mv"
          style:left="{Math.round(panelPos.x - PANEL_W / 2)}px"
          style:top="{Math.round(panelPos.y - PANEL_H / 2)}px"
          style:width="{PANEL_W}px"
          style:height="{PANEL_H}px"
          onpointerdown={(e) => begin(e, 'panel', '', panelPos.x, panelPos.y)}
        >
          <div class="t"><i>&gt;_</i> Linager</div>
          <div class="s">{$tr('scheme.panel')} · {$tr('scheme.online', { n: online, m: total })}</div>
        </div>
        {#each rows as r (r.card.id)}
          {@render node(r)}
        {/each}
      </div>
    {/if}

    {#if showPlane}
      <div class="zoom">
        <span class="pct">{pct}%</span>
        <button type="button" class="btn sm sqb" aria-label={$tr('scheme.zoom.out')} title={$tr('scheme.zoom.out')} onclick={() => zoomCentre(1 / 1.25)}>&minus;</button>
        <button type="button" class="btn sm sqb" aria-label={$tr('scheme.zoom.in')} title={$tr('scheme.zoom.in')} onclick={() => zoomCentre(1.25)}>+</button>
        <button type="button" class="btn sm" title={$tr('scheme.zoom.oneTitle')} onclick={() => zoomCentre(1 / view.k)}>{$tr('scheme.zoom.one')}</button>
        <button type="button" class="btn sm" title={$tr('scheme.zoom.fitTitle')} onclick={fitNow}>{$tr('scheme.zoom.fit')}</button>
      </div>
    {/if}
  </div>

  <div class="legend">
    <span><i class="sq"></i>{$tr('scheme.legend.ok')}</span>
    <span><i class="sq w"></i>{$tr('scheme.legend.warn')}</span>
    <span><i class="sq c"></i>{$tr('scheme.legend.crit')}</span>
    <span><i class="sq off"></i>{$tr('scheme.legend.off')}</span>
    <span>· {$tr('scheme.legend.speed')}</span>
    <span>· {$tr('scheme.legend.large')}</span>
  </div>

  <div class="detail">
    {#if selCard}
      {@const s = selCard.online ? selCard.sample : null}
      {@const groupId = selCard.group_id}
      <span class="nm">{selCard.name}</span>
      <span class="info">
        {selCard.address} · {selCard.os} · {$tr('dash.agent')} {selCard.agent_version}{#if groupId !== null} · {groupName(groupId)}{/if}
      </span>
      {#if !selCard.online}
        <span class="info">{$tr('scheme.noData')}</span>
      {:else}
        <span class="m">
          {@render metric($tr('dash.cpu'), s ? s.cpu : null)}
          {@render metric($tr('dash.ram'), s ? s.mem_pct : null)}
          {@render metric($tr('dash.disk'), s ? s.disk_pct : null)}
          <span>{$tr('scheme.net')} <b>{s ? fmtRate(s.rx_bps + s.tx_bps) : '—'}</b></span>
        </span>
      {/if}
      <span class="acts">
        <button type="button" class="btn sm primary" onclick={() => selCard && onopen(selCard.name)}>{$tr('dash.open')}</button>
        <button type="button" class="btn sm" disabled={!selCard.online} onclick={() => selCard && openTermWindow(selCard.id, selCard.name)}>{$tr('dash.terminal')} &gt;_</button>
      </span>
    {:else}
      <span class="hint">{$tr(vertical ? 'scheme.hintPhone' : 'scheme.hint')}</span>
    {/if}
  </div>
</section>

<style>
  .scheme {
    padding: 0;
    min-width: 0;
  }

  header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    padding: 10px 14px;
    border-bottom: 2px solid var(--line);
  }

  header h3 {
    margin: 0;
    font-family: var(--font-display);
    font-weight: 400;
    font-size: 15px;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: var(--muted);
  }

  header .meta {
    flex: 1;
    font-size: 12px;
    color: var(--muted);
  }

  .btn.sm {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 6px;
    height: 28px;
    padding: 0 8px;
    font-size: 13px;
    box-shadow: 2px 2px 0 var(--shade);
  }

  .btn.sm:disabled {
    box-shadow: none;
  }

  .btn.sqb {
    min-width: 28px;
    padding: 0 6px;
  }

  .stage {
    position: relative;
    /* the z-indexes of the nodes stay inside the plane: they must not climb over the pinned toolbar of a phone */
    isolation: isolate;
    overflow: hidden;
    background-image: radial-gradient(var(--seg-off) 1px, transparent 1px);
    background-size: 20px 20px;
  }

  .stage.mv {
    cursor: grab;
    touch-action: none;
  }

  .world {
    position: absolute;
    left: 0;
    top: 0;
    width: 0;
    height: 0;
    transform-origin: 0 0;
  }

  .lines {
    position: absolute;
    left: 0;
    top: 0;
    width: 1px;
    height: 1px;
    overflow: visible;
    pointer-events: none;
  }

  .zoom {
    position: absolute;
    right: 10px;
    top: 10px;
    z-index: 6;
    display: flex;
    align-items: center;
    gap: 6px;
    cursor: default;
  }

  .zoom .pct {
    min-width: 44px;
    padding: 0 4px;
    font-size: 12px;
    color: var(--muted);
    text-align: right;
    background: var(--surface);
  }

  /* Links */
  .lk {
    fill: none;
    stroke-width: 3;
    stroke-linecap: butt;
    vector-effect: non-scaling-stroke;
  }

  .lk.run {
    stroke-dasharray: 3 13;
    animation: flow 1.1s linear infinite;
  }

  .lk.s1 {
    animation-duration: 2.4s;
  }

  .lk.s2 {
    animation-duration: 1.1s;
  }

  .lk.s3 {
    animation-duration: 0.5s;
  }

  .lk.hot {
    stroke-width: 4;
  }

  .lk.ok {
    stroke: var(--good);
  }

  .lk.warn {
    stroke: var(--warn);
  }

  .lk.crit {
    stroke: var(--crit);
  }

  .lk.off {
    stroke: var(--muted);
    stroke-dasharray: 2 8;
    opacity: 0.55;
  }

  .lk.gray {
    stroke: var(--muted);
    stroke-dasharray: 2 8;
    opacity: 0.3;
    animation: none;
  }

  @keyframes flow {
    to {
      stroke-dashoffset: -16;
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .lk.run {
      animation: none;
    }
  }

  /* Nodes */
  .n,
  .pn {
    position: absolute;
    overflow: hidden;
    padding: 0;
    text-align: left;
    background: var(--surface);
    color: var(--ink);
    font: inherit;
    border: 2px solid var(--line);
    border-radius: 0;
    box-shadow: 3px 3px 0 var(--shade);
    user-select: none;
    -webkit-user-select: none;
  }

  .n {
    z-index: 3;
    cursor: pointer;
  }

  .n:hover {
    box-shadow: 3px 3px 0 var(--ink);
  }

  .n.sel,
  .n:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }

  .n.gray {
    filter: grayscale(1);
    opacity: 0.5;
  }

  .stage.mv .n,
  .stage.mv .pn.mv,
  .stage.mv .gf span {
    touch-action: none;
    cursor: grab;
  }

  .stage.mv .gf span {
    pointer-events: auto;
  }

  .n.full {
    display: flex;
    flex-direction: column;
    justify-content: center;
    gap: 1px;
    padding: 7px 10px 5px;
  }

  .n.full::before {
    content: '';
    position: absolute;
    left: 0;
    right: 0;
    top: 0;
    height: 4px;
    background: transparent;
  }

  .n.st-warn::before {
    background: var(--warn);
  }

  .n.st-crit::before {
    background: var(--crit);
  }

  .n.st-off::before {
    background: var(--muted);
  }

  .nm {
    margin-top: 2px;
    overflow: hidden;
    font-family: var(--font-display);
    font-weight: 400;
    font-size: 14px;
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .stt {
    overflow: hidden;
    font-size: 12px;
    color: var(--muted);
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .n.st-warn .stt {
    color: var(--warn);
  }

  .n.st-crit .stt {
    color: var(--crit);
  }

  .n.st-off {
    border-color: var(--muted);
    color: var(--muted);
  }

  .sq {
    display: inline-block;
    width: 8px;
    height: 8px;
    margin-right: 6px;
    background: var(--good);
  }

  .st-warn .sq,
  .legend .w {
    background: var(--warn);
  }

  .st-crit .sq,
  .legend .c {
    background: var(--crit);
  }

  .st-off .sq,
  .legend .off {
    background: var(--muted);
  }

  .n.comp {
    display: flex;
    align-items: center;
    padding: 0 8px;
  }

  .n.comp .nm {
    margin: 0;
    font-size: 13px;
  }

  .n.comp.st-warn {
    border-left: 5px solid var(--warn);
  }

  .n.comp.st-crit {
    border-left: 5px solid var(--crit);
  }

  .pn {
    z-index: 4;
    display: flex;
    flex-direction: column;
    justify-content: center;
    padding: 8px 12px 6px;
    cursor: default;
    box-shadow: 4px 4px 0 var(--shade);
  }

  .pn::before {
    content: '';
    position: absolute;
    left: 0;
    right: 0;
    top: 0;
    height: 4px;
    background: var(--accent);
  }

  .lsw {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px;
  }

  .lsw .lbl {
    font-size: 12px;
    color: var(--muted);
  }

  .lsw .lbl.warn {
    color: var(--warn);
  }

  .lsw .chip {
    padding: 2px 8px;
    font-size: 13px;
  }

  .pn .t {
    margin-top: 3px;
    font-family: var(--font-display);
    font-variation-settings: 'ELSH' 40;
    font-size: 19px;
  }

  .pn .t i {
    font-style: normal;
    color: var(--accent);
  }

  /* two short lines are better than a cut one: the panel box has a fixed size on the plane */
  .pn .s {
    overflow: hidden;
    font-size: 12px;
    line-height: 1.3;
    color: var(--muted);
  }

  .gf {
    position: absolute;
    z-index: 1;
    border: 2px dashed var(--seg-off);
    pointer-events: none;
  }

  .gf.gray {
    opacity: 0.5;
  }

  .gf span {
    position: absolute;
    left: 8px;
    top: -11px;
    padding: 0 6px;
    font-family: var(--font-display);
    font-size: 12px;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--muted);
    background: var(--surface);
    user-select: none;
    -webkit-user-select: none;
  }

  .gh {
    position: absolute;
    z-index: 2;
    font-family: var(--font-display);
    font-size: 12px;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--muted);
  }

  /* Empty states */
  .void {
    display: flex;
    flex-direction: column;
    align-items: center;
    padding-top: 43px;
  }

  .void.none {
    padding-top: 73px;
  }

  .pn.static {
    position: relative;
    flex: none;
    width: 200px;
    height: 74px;
  }

  .vline {
    flex: none;
    width: 0;
    height: 103px;
    border-left: 3px dashed var(--muted);
    opacity: 0.55;
  }

  .ph {
    flex: none;
    box-sizing: border-box;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 6px;
    width: 240px;
    height: 92px;
    padding: 8px;
    text-align: center;
    background: var(--surface);
    border: 2px dashed var(--muted);
  }

  .note {
    margin: 36px 0 0;
    font-size: 13px;
    color: var(--muted);
    text-align: center;
  }

  /* Legend and detail strip */
  .legend {
    display: flex;
    flex-wrap: wrap;
    gap: 6px 16px;
    padding: 10px 14px;
    border-top: 2px solid var(--line);
    font-size: 12px;
    color: var(--muted);
  }

  .detail {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px 18px;
    min-height: 56px;
    padding: 10px 14px;
    border-top: 2px solid var(--line);
  }

  .detail .nm {
    margin: 0;
    overflow: visible;
    font-variation-settings: 'ELSH' 40;
    font-size: 18px;
    white-space: normal;
  }

  .detail .info {
    font-size: 12px;
    color: var(--muted);
    overflow-wrap: anywhere;
  }

  .detail .m {
    display: flex;
    flex-wrap: wrap;
    gap: 4px 14px;
    font-size: 12px;
  }

  .detail .m b {
    font-weight: 400;
    color: var(--ink);
  }

  .detail .m b.warn {
    color: var(--warn);
  }

  .detail .m b.crit {
    color: var(--crit);
  }

  .acts {
    display: flex;
    gap: 8px;
    margin-left: auto;
  }

  .hint {
    font-size: 12px;
    color: var(--muted);
  }

  @media (max-width: 720px) {
    .acts {
      width: 100%;
      margin-left: 0;
    }
  }

  :global(body.scheme-drag),
  :global(body.scheme-drag *) {
    cursor: grabbing !important;
    user-select: none !important;
  }
</style>
