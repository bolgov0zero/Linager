<script lang="ts">
  import { onMount } from 'svelte';
  import { api, type FwLogEvent, type FwSet } from '../../../api';
  import { locale, tr as T } from '../../../i18n';
  import Modal from '../../Modal.svelte';
  import Select from '../../ui/Select.svelte';
  import { formatDateTime } from '../format';
  import { canLog, ipInList, isLogged, parseRule, ruleLogId, stripLog } from './fw-view';

  type Chain = 'input' | 'forward' | 'output';
  interface Known {
    chain: Chain;
    index: number;
    text: string;
  }

  let {
    serverId,
    rules,
    sets,
    onToggle,
    onToggleDrops,
    onAddToList,
    onBlock,
    onOpenRule,
    onclose,
  }: {
    serverId: string;
    /** The rules of the draft with their position in the chain. */
    rules: Known[];
    sets: FwSet[];
    onToggle: (chain: Chain, index: number, on: boolean) => void;
    onToggleDrops: () => void;
    onAddToList: (list: string, ip: string) => string | null;
    onBlock: (ip: string) => Promise<string | null>;
    onOpenRule: (chain: Chain) => void;
    onclose: () => void;
  } = $props();

  const POLL_MS = 5000;

  let events = $state<FwLogEvent[]>([]);
  let loaded = $state(false);
  let available = $state(true);
  let truncated = $state(false);
  let live = $state(true);
  let now = $state(Math.floor(Date.now() / 1000));

  let period = $state(60);
  let chains = $state<Record<string, boolean>>({});
  let actions = $state<Record<string, boolean>>({});
  let protos = $state<Record<string, boolean>>({});
  let port = $state('');
  let source = $state('');
  let group = $state(false);
  let limit = $state(12);
  let openKey = $state<string | null>(null);
  let note = $state<string | null>(null);
  let pickList = $state('');
  let busy = $state(false);

  const loggedRules = $derived(rules.filter((r) => isLogged(r.text)));
  const byId = $derived(new Map(rules.map((r) => [ruleLogId(r.text), r])));
  const elementsOf = $derived(new Map(sets.map((s) => [s.name, s.elements])));

  async function load(): Promise<void> {
    try {
      const res = await api.firewallLog(serverId);
      events = res.events;
      available = res.available;
      truncated = !!res.truncated;
    } catch {
      /* keep what is shown */
    } finally {
      loaded = true;
      now = Math.floor(Date.now() / 1000);
    }
  }

  onMount(() => {
    void load();
    const timer = setInterval(() => {
      if (live && loggedRules.length > 0) void load();
    }, POLL_MS);
    return () => clearInterval(timer);
  });

  // ---- filters ----

  function portOk(p: number | undefined, expr: string): boolean {
    const q = expr.trim();
    if (!q) return true;
    if (!p) return false;
    return q.split(/[,\s]+/).filter(Boolean).some((x) => {
      const m = x.match(/^(\d+)-(\d+)$/);
      return m ? p >= Number(m[1]) && p <= Number(m[2]) : String(p) === x;
    });
  }

  /** Lists an address belongs to, for the `@list` search and the tag in the table. */
  function listsOf(ip: string): string[] {
    return [...elementsOf].filter(([, els]) => ipInList(ip, els)).map(([name]) => name);
  }

  function sourceOk(e: FwLogEvent, q: string): boolean {
    const t = q.trim().toLowerCase();
    if (!t) return true;
    if (t.startsWith('@')) return listsOf(e.src).some((n) => `@${n}`.startsWith(t));
    return e.src.toLowerCase().includes(t);
  }

  const chosen = (o: Record<string, boolean>): string[] => Object.keys(o).filter((k) => o[k]);

  /** Events that pass every filter except the group `skip` (for the counters on the chips). */
  function pass(skip: 'chain' | 'action' | 'proto' | null): FwLogEvent[] {
    const ch = chosen(chains);
    const ac = chosen(actions);
    const pr = chosen(protos);
    const from = now - period * 60;
    return events.filter(
      (e) =>
        e.time >= from &&
        (skip === 'chain' || ch.length === 0 || ch.includes(e.chain)) &&
        (skip === 'action' || ac.length === 0 || ac.includes(e.action)) &&
        (skip === 'proto' || pr.length === 0 || pr.includes(e.proto)) &&
        portOk(e.dport, port) &&
        sourceOk(e, source),
    );
  }

  const inPeriod = $derived(events.filter((e) => e.time >= now - period * 60));
  const rows = $derived(pass(null));
  const activeFilters = $derived(chosen(chains).length + chosen(actions).length + chosen(protos).length + (port.trim() ? 1 : 0) + (source.trim() ? 1 : 0));
  const count = (skip: 'chain' | 'action' | 'proto', key: 'chain' | 'action' | 'proto', value: string): number =>
    pass(skip).filter((e) => e[key] === value).length;

  interface Line {
    key: string;
    e: FwLogEvent;
    n: number;
  }

  const lines = $derived.by<Line[]>(() => {
    if (!group) return rows.map((e, i) => ({ key: `${e.time}:${i}`, e, n: 1 }));
    const map = new Map<string, Line>();
    for (const e of rows) {
      const k = [e.chain, e.action, e.proto, e.src, e.dport ?? 0].join('|');
      const line = map.get(k);
      if (line) line.n++;
      else map.set(k, { key: k, e, n: 1 });
    }
    return [...map.values()].sort((a, b) => b.n - a.n || b.e.time - a.e.time);
  });
  const shownLines = $derived(lines.slice(0, limit));

  // ---- the histogram of what passes the filters ----
  const BARS = 48;
  const bars = $derived.by(() => {
    const span = period * 60;
    const cols = Array.from({ length: BARS }, () => ({ a: 0, d: 0 }));
    for (const e of rows) {
      const i = Math.min(BARS - 1, Math.floor(((e.time - (now - span)) / span) * BARS));
      if (i >= 0) cols[i][e.action === 'accept' ? 'a' : 'd']++;
    }
    const max = Math.max(1, ...cols.map((c) => c.a + c.d));
    return cols.map((c) => ({ ...c, ha: (c.a / max) * 100, hd: (c.d / max) * 100 }));
  });

  const stats = $derived({
    total: inPeriod.length,
    blocked: inPeriod.filter((e) => e.action !== 'accept').length,
    allowed: inPeriod.filter((e) => e.action === 'accept').length,
    sources: new Set(inPeriod.map((e) => e.src)).size,
  });

  function reset(): void {
    chains = {};
    actions = {};
    protos = {};
    port = '';
    source = '';
    limit = 12;
  }

  function toggle(o: Record<string, boolean>, key: string): void {
    o[key] = !o[key];
    limit = 12;
  }

  function ago(ts: number): string {
    const m = Math.round((now - ts) / 60);
    if (m < 1) return $T('fw.log.justNow');
    if (m < 60) return $T('fw.log.minAgo', { n: m });
    if (m < 1440) return $T('fw.log.hourAgo', { n: Math.round(m / 60) });
    return $T('fw.log.dayAgo', { n: Math.round(m / 1440) });
  }

  function ruleName(e: FwLogEvent): { label: string; known: Known | null } {
    const k = byId.get(e.rule) ?? null;
    return { label: k ? stripLog(k.text) : e.rule, known: k };
  }

  const exportCsv = (): void => {
    const head = ['time', 'chain', 'action', 'proto', 'src', 'dport', 'dst', 'iface', 'len', 'flags', 'rule'];
    const body = rows.map((e) => [new Date(e.time * 1000).toISOString(), e.chain, e.action, e.proto, e.src, e.dport ?? '', e.dst, e.iface, e.len ?? '', e.flags ?? '', ruleName(e).label]);
    const csv = [head, ...body].map((r) => r.map((c) => `"${String(c).replace(/"/g, '""')}"`).join(',')).join('\n');
    const url = URL.createObjectURL(new Blob([csv], { type: 'text/csv' }));
    const a = document.createElement('a');
    a.href = url;
    a.download = 'firewall-log.csv';
    a.click();
    URL.revokeObjectURL(url);
  };

  async function block(ip: string): Promise<void> {
    if (busy) return;
    busy = true;
    note = await onBlock(ip);
    if (!note) note = $T('fw.log.blocked', { ip });
    busy = false;
  }

  function addList(ip: string): void {
    const name = pickList || sets[0]?.name;
    if (!name) return;
    note = onAddToList(name, ip) ?? $T('fw.log.addedToList', { ip, list: name });
  }

  const actLabel = (a: string): string => $T(`fw.log.act.${a}`);
  const listOptions = $derived(sets.map((s) => ({ value: s.name, label: `@${s.name}` })));
</script>

<Modal title={$T('fw.log.title')} labelId="fw-log-title" wide {onclose}>
  {#if !loaded}
    <p class="muted">{$T('common.loading')}</p>
  {:else if !available}
    <p class="muted">{$T('fw.log.unavailable')}</p>
  {:else if events.length === 0}
    <div class="howto">
      <p>{$T('fw.log.howto')}</p>
      <div class="steps">
        <div class="step"><b>1</b><span>{$T('fw.log.step1')}</span></div>
        <div class="step"><b>2</b><span>{$T('fw.log.step2')}</span></div>
        <div class="step"><b>3</b><span>{$T('fw.log.step3')}</span></div>
      </div>
      <div class="rules">
        {#each rules as r (r.chain + r.index + r.text)}
          {@const v = parseRule(r.text)}
          <div class="r">
            <button class="sw" type="button" aria-pressed={v.logged} disabled={!canLog(r.text)} aria-label={$T('fw.log.toggle')} onclick={() => onToggle(r.chain, r.index, !v.logged)}></button>
            <span class="mono small">{r.chain} · {stripLog(r.text)}</span>
            <span class="act {v.action}">{$T(`fw.act.${v.action}`)}</span>
          </div>
        {:else}
          <p class="muted small pad">{$T('fw.chain.empty')}</p>
        {/each}
      </div>
      <div class="row between">
        <span class="small muted">{$T('fw.log.limitNote')}</span>
        <button class="btn" type="button" onclick={onToggleDrops}>{$T('fw.log.enableDrops')}</button>
      </div>
      <p class="small muted">{loggedRules.length > 0 ? $T('fw.log.markedNote') : $T('fw.log.applyNote')}</p>
    </div>
  {:else}
    <div class="row between">
      <div class="row" role="group" aria-label={$T('fw.log.period')}>
        {#each [15, 60, 1440, 10080] as p (p)}
          <button class="chip" type="button" aria-pressed={period === p} onclick={() => { period = p; limit = 12; }}>{$T(`fw.log.p.${p}`)}</button>
        {/each}
        <button class="chip live" type="button" aria-pressed={live} onclick={() => (live = !live)}><i></i>{$T('fw.log.live')}</button>
      </div>
      <div class="stats">
        <div class="stat"><b>{stats.total}</b><span>{$T('fw.log.stat.total')}</span></div>
        <div class="stat"><b class="crit">{stats.blocked}</b><span>{$T('fw.log.stat.blocked')}</span></div>
        <div class="stat"><b class="ok">{stats.allowed}</b><span>{$T('fw.log.stat.allowed')}</span></div>
        <div class="stat"><b>{stats.sources}</b><span>{$T('fw.log.stat.sources')}</span></div>
      </div>
    </div>

    <div>
      <div class="hist" role="img" aria-label={$T('fw.log.hist')}>
        {#each bars as c, i (i)}
          <div class="col" title={String(c.a + c.d)}><i class="d" style:height="{c.hd}%"></i><i class="a" style:height="{c.ha}%"></i></div>
        {/each}
      </div>
      <div class="axis">
        <span>{$T(`fw.log.ago.${period}`)}</span>
        <span class="row legend"><span class="ok">■ {$T('fw.log.legend.allowed')}</span><span class="crit">■ {$T('fw.log.legend.blocked')}</span></span>
        <span>{$T('fw.log.now')}</span>
      </div>
    </div>

    <div class="filters">
      <div class="fg g-chain">
        <span class="lbl">{$T('fw.col.chain')}</span>
        <div class="row">
          {#each ['input', 'forward', 'output'] as c (c)}
            <button class="chip" type="button" aria-pressed={!!chains[c]} onclick={() => toggle(chains, c)}>{c} <span class="count">{count('chain', 'chain', c)}</span></button>
          {/each}
        </div>
      </div>
      <div class="fg g-act">
        <span class="lbl">{$T('fw.col.action')}</span>
        <div class="row">
          {#each ['accept', 'drop', 'reject'] as a (a)}
            <button class="chip {a}" type="button" aria-pressed={!!actions[a]} onclick={() => toggle(actions, a)}>{actLabel(a)} <span class="count">{count('action', 'action', a)}</span></button>
          {/each}
        </div>
      </div>
      <div class="fg g-proto">
        <span class="lbl">{$T('fw.col.proto')}</span>
        <div class="row">
          {#each ['TCP', 'UDP', 'ICMP'] as p (p)}
            <button class="chip" type="button" aria-pressed={!!protos[p]} onclick={() => toggle(protos, p)}>{p} <span class="count">{count('proto', 'proto', p)}</span></button>
          {/each}
        </div>
      </div>
      <label class="fg g-port">
        <span class="lbl">{$T('fw.log.port')}</span>
        <input class="input mono" type="text" autocomplete="off" spellcheck="false" placeholder="22, 80, 443, 1000-2000" bind:value={port} oninput={() => (limit = 12)} />
      </label>
      <label class="fg g-src">
        <span class="lbl">{$T('fw.col.source')}</span>
        <input class="input mono" type="search" autocomplete="off" spellcheck="false" placeholder={$T('fw.log.sourcePh')} bind:value={source} oninput={() => (limit = 12)} />
      </label>
      <div class="fg g-reset">
        <span class="hint">{$T('fw.log.logic')}</span>
        <button class="btn" type="button" disabled={activeFilters === 0} onclick={reset}>{$T('fw.log.reset')}</button>
      </div>
    </div>

    <div class="toolbar">
      <div>
        {group ? $T('fw.log.foundGroups', { n: lines.length }) : $T('fw.log.foundOf', { n: lines.length, total: inPeriod.length })}
        {#if activeFilters > 0}<span class="muted">· {$T('fw.log.filters', { n: activeFilters })}</span>{/if}
      </div>
      <div class="row">
        <button class="chip" type="button" aria-pressed={group} title={$T('fw.log.groupHint')} onclick={() => { group = !group; limit = 12; openKey = null; }}>{$T('fw.log.group')}</button>
        <button class="btn" type="button" disabled={rows.length === 0} onclick={exportCsv}>{$T('fw.log.csv')}</button>
      </div>
    </div>
    {#if note}<p class="small note-line" role="status">{note}</p>{/if}

    <div class="tw">
      <table class="tbl">
        <thead>
          <tr>
            <th>{$T('fw.log.col.time')}</th>
            <th>{$T('fw.col.chain')}</th>
            <th>{$T('fw.col.action')}</th>
            <th>{$T('fw.col.proto')}</th>
            <th>{$T('fw.col.source')}</th>
            <th>{$T('fw.col.port')}</th>
            <th>{$T('fw.log.col.rule')}</th>
            <th class="num">{group ? $T('fw.log.col.times') : $T('fw.log.col.packets')}</th>
          </tr>
        </thead>
        <tbody>
          {#each shownLines as l (l.key)}
            {@const e = l.e}
            {@const rn = ruleName(e)}
            {@const open = openKey === l.key}
            <tr class="ev" class:open onclick={() => (openKey = open ? null : l.key)}>
              <td class="nowrap" title={formatDateTime(e.time, $locale)}><span class="caret"></span>{group ? $T('fw.log.last', { when: ago(e.time) }) : ago(e.time)}</td>
              <td class="mono">{e.chain}</td>
              <td><span class="act {e.action}">{actLabel(e.action)}</span></td>
              <td class="mono">{e.proto}</td>
              <td class="mono"><span class="srcc">{e.src}{#each listsOf(e.src).slice(0, 2) as n (n)}<span class="tag">@{n}</span>{/each}</span></td>
              <td class="mono">{e.dport ?? '—'}</td>
              <td class="rl" title={rn.label}>{rn.known ? `#${rn.known.index + 1} ` : ''}{rn.label.length > 30 ? rn.label.slice(0, 29) + '…' : rn.label}</td>
              <td class="num">{l.n}</td>
            </tr>
            {#if open}
              <tr class="det">
                <td colspan="8">
                  <div class="detbox">
                    <div class="kv">
                      <span><b>{$T('fw.log.col.time')}</b>{formatDateTime(e.time, $locale)}</span>
                      <span><b>{$T('fw.log.iface')}</b>{e.iface || '—'}</span>
                      <span><b>{$T('fw.log.dst')}</b>{e.dst}{e.dport ? `:${e.dport}` : ''}</span>
                      {#if e.len}<span><b>{$T('fw.log.len')}</b>{e.len} B</span>{/if}
                      {#if e.flags}<span><b>{$T('fw.log.flags')}</b>{e.flags}</span>{/if}
                      <span><b>{$T('fw.log.col.rule')}</b>{rn.label}</span>
                    </div>
                    <div class="row" onclick={(ev) => ev.stopPropagation()} role="presentation">
                      {#if sets.length > 0}
                        <Select size="sm" value={pickList || sets[0].name} ariaLabel={$T('fw.log.pickList')} options={listOptions} onchange={(v) => (pickList = v)} />
                        <button class="btn sm" type="button" onclick={() => addList(e.src)}>{$T('fw.log.toList')}</button>
                      {/if}
                      <button class="btn sm danger" type="button" disabled={busy} onclick={() => void block(e.src)}>{$T('fw.log.block')}</button>
                      {#if rn.known}<button class="btn sm" type="button" onclick={() => onOpenRule(rn.known!.chain)}>{$T('fw.log.openRule')}</button>{/if}
                    </div>
                  </div>
                </td>
              </tr>
            {/if}
          {:else}
            <tr><td colspan="8" class="empty">{$T('fw.log.nothing')} <button class="btn sm" type="button" onclick={reset}>{$T('fw.log.reset')}</button></td></tr>
          {/each}
        </tbody>
      </table>
    </div>
    <div class="toolbar">
      <span class="small muted">{$T('fw.log.shown', { n: shownLines.length, total: lines.length })}{#if truncated} · {$T('fw.log.truncated')}{/if}</span>
      {#if shownLines.length < lines.length}<button class="btn sm" type="button" onclick={() => (limit += 15)}>{$T('fw.log.more')}</button>{/if}
    </div>
  {/if}
  <div class="row end foot">
    <span class="small muted grow">{$T('fw.log.source')}</span>
    <button class="btn" type="button" onclick={onclose}>{$T('common.close')}</button>
  </div>
</Modal>

<style>
  p {
    margin: 0;
  }

  .grow {
    flex: 1;
  }

  .foot {
    margin-top: 4px;
  }

  .stats {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
  }

  .stat {
    display: flex;
    flex-direction: column;
    min-width: 100px;
    padding: 6px 12px;
    border: 2px solid var(--line);
  }

  .stat b {
    font-family: var(--font-display);
    font-size: 22px;
    font-weight: 400;
    line-height: 1.2;
  }

  .stat span {
    font-size: 12px;
    color: var(--muted);
  }

  .ok {
    color: var(--good);
  }

  .crit {
    color: var(--crit);
  }

  .hist {
    display: flex;
    align-items: flex-end;
    gap: 2px;
    height: 64px;
    padding-top: 6px;
  }

  .hist .col {
    display: flex;
    flex: 1;
    flex-direction: column;
    justify-content: flex-end;
    height: 100%;
    min-width: 2px;
  }

  .hist .a {
    background: var(--good);
  }

  .hist .d {
    background: var(--crit);
  }

  .axis {
    display: flex;
    justify-content: space-between;
    font-size: 11px;
    color: var(--muted);
  }

  .legend {
    gap: 12px;
  }

  .chip,
  .btn {
    display: inline-flex;
    align-items: center;
    height: 36px;
    padding-block: 0;
  }

  .btn.sm {
    height: 28px;
  }

  .chip .count {
    margin-left: 4px;
    color: var(--muted);
  }

  .chip[aria-pressed='true'] .count {
    color: inherit;
    opacity: 0.75;
  }

  .chip.accept[aria-pressed='true'] {
    background: var(--good);
    border-color: var(--good);
    color: var(--surface);
  }

  .chip.drop[aria-pressed='true'] {
    background: var(--crit);
    border-color: var(--crit);
    color: var(--surface);
  }

  .chip.reject[aria-pressed='true'] {
    background: var(--warn);
    border-color: var(--warn);
    color: var(--surface);
  }

  .live i {
    display: inline-block;
    width: 8px;
    height: 8px;
    margin-right: 8px;
    background: var(--muted);
  }

  .live[aria-pressed='true'] i {
    background: var(--good);
    animation: pulse 1.4s infinite;
  }

  @keyframes pulse {
    50% {
      opacity: 0.25;
    }
  }

  .filters {
    display: grid;
    grid-template-columns: repeat(12, minmax(0, 1fr));
    gap: 12px 16px;
    padding: 12px 14px;
    border: 2px solid var(--line);
  }

  .fg {
    display: flex;
    flex-direction: column;
    gap: 6px;
    min-width: 0;
  }

  .fg .lbl {
    font-size: 11px;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--muted);
  }

  .fg .row {
    gap: 6px;
  }

  .g-chain {
    grid-column: span 4;
  }

  .g-act {
    grid-column: span 5;
  }

  .g-proto {
    grid-column: span 3;
  }

  .g-port {
    grid-column: span 4;
  }

  .g-src {
    grid-column: span 5;
  }

  .g-reset {
    grid-column: span 3;
    flex-direction: row;
    align-items: flex-end;
    justify-content: flex-end;
    gap: 10px;
  }

  .fg .input {
    width: 100%;
    height: 36px;
    padding-block: 0;
  }

  .hint {
    font-size: 12px;
    color: var(--muted);
  }

  .toolbar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
  }

  .note-line {
    padding: 6px 10px;
    border: 2px solid var(--accent);
  }

  .tw {
    max-height: 420px;
    overflow: auto;
  }

  .tbl {
    width: 100%;
    border-collapse: collapse;
    font-size: 13px;
  }

  .tbl th {
    padding: 6px 10px;
    border-bottom: 2px solid var(--line);
    font-family: var(--font-display);
    font-size: 12px;
    font-weight: 400;
    letter-spacing: 0.06em;
    text-align: left;
    text-transform: uppercase;
    white-space: nowrap;
    color: var(--muted);
  }

  .tbl td {
    padding: 8px 10px;
    border-bottom: 1px dashed var(--seg-off);
    vertical-align: middle;
  }

  .num {
    text-align: right;
  }

  .nowrap {
    white-space: nowrap;
  }

  .act {
    display: inline-block;
    padding: 1px 8px;
    border: 2px solid var(--muted);
    color: var(--muted);
    font-size: 12px;
    white-space: nowrap;
  }

  .act.accept {
    border-color: var(--good);
    color: var(--good);
  }

  .act.drop {
    border-color: var(--crit);
    color: var(--crit);
  }

  .act.reject {
    border-color: var(--warn);
    color: var(--warn);
  }

  .srcc {
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }

  .tag {
    padding: 0 6px;
    border: 2px solid var(--accent);
    color: var(--accent);
    font-size: 11px;
  }

  .rl {
    color: var(--muted);
    font-size: 12px;
    white-space: nowrap;
  }

  tr.ev {
    cursor: pointer;
  }

  tr.ev:hover td {
    background: color-mix(in srgb, var(--accent) 7%, transparent);
  }

  tr.ev.open td {
    background: color-mix(in srgb, var(--accent) 11%, transparent);
    border-bottom-color: transparent;
  }

  tr.det td {
    padding: 0 10px 12px;
    background: color-mix(in srgb, var(--accent) 11%, transparent);
  }

  .detbox {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 8px 28px;
    padding: 10px 12px;
    border: 2px dashed var(--line);
    background: var(--surface);
  }

  .kv {
    display: flex;
    flex-wrap: wrap;
    gap: 6px 22px;
    font-size: 12px;
  }

  .kv b {
    margin-right: 6px;
    font-weight: 400;
    color: var(--muted);
  }

  .caret {
    display: inline-block;
    width: 0;
    height: 0;
    margin-right: 6px;
    border-left: 4px solid transparent;
    border-right: 4px solid transparent;
    border-top: 5px solid currentColor;
    transform: rotate(-90deg);
    transition: transform 0.15s;
  }

  tr.open .caret {
    transform: none;
  }

  .empty {
    padding: 26px 10px;
    text-align: center;
    color: var(--muted);
  }

  .howto {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .steps {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
    gap: 10px;
  }

  .step {
    display: flex;
    flex-direction: column;
    gap: 4px;
    padding: 10px 12px;
    border: 2px solid var(--line);
  }

  .step b {
    font-family: var(--font-display);
    font-size: 20px;
    font-weight: 400;
    color: var(--accent);
  }

  .rules {
    border: 2px solid var(--line);
  }

  .rules .r {
    display: grid;
    grid-template-columns: auto 1fr auto;
    gap: 12px;
    align-items: center;
    padding: 8px 12px;
    border-bottom: 1px dashed var(--seg-off);
  }

  .pad {
    padding: 10px 12px;
  }

  .sw {
    position: relative;
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

  .sw:disabled {
    opacity: 0.4;
    cursor: not-allowed;
  }

  @media (max-width: 900px) {
    .g-chain,
    .g-act,
    .g-proto,
    .g-port,
    .g-src,
    .g-reset {
      grid-column: span 12;
    }
  }
</style>
