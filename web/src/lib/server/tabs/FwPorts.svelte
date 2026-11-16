<script lang="ts">
  import { untrack } from 'svelte';
  import { api, ApiError, type FwListener, type FwPorts, type FwSet } from '../../../api';
  import { tr as trs } from '../../../i18n';
  import { isPhone } from '../../ui/viewport';
  import SourceInput from './SourceInput.svelte';
  import { buildRule, type ListRef } from './fw-rule';
  import './phone-c.css';

  let {
    serverId,
    lists,
    onrules,
  }: {
    serverId: string;
    lists: FwSet[];
    /** Adds rules to the input chain of the draft; returns an error text or null. */
    onrules: (texts: string[]) => Promise<string | null>;
  } = $props();

  type Filter = 'all' | 'out' | 'local' | 'norule';
  type Mode = 'close' | 'restrict';

  const POLL_MS = 15000;

  let data = $state<FwPorts | null>(null);
  let failed = $state(false);
  let loading = $state(false);
  let filter = $state<Filter>('all');
  let plan = $state<{ l: FwListener; mode: Mode } | null>(null);
  let source = $state('');
  let planError = $state<string | null>(null);
  let planBusy = $state(false);

  const all = $derived(data?.listeners ?? []);
  const outside = (l: FwListener): boolean => l.scope !== 'local';
  const counts = $derived({
    all: all.length,
    out: all.filter(outside).length,
    local: all.filter((l) => !outside(l)).length,
    norule: all.filter((l) => l.firewall === 'open').length,
  });
  const rows = $derived(
    all.filter((l) => (filter === 'out' ? outside(l) : filter === 'local' ? !outside(l) : filter === 'norule' ? l.firewall === 'open' : true)),
  );
  const listRefs = $derived<ListRef[]>(lists.map((s) => ({ name: s.name, family: s.family })));

  async function load(id: string): Promise<void> {
    loading = true;
    try {
      const next = await api.firewallPorts(id);
      if (id !== serverId) return;
      data = next;
      failed = false;
    } catch (e) {
      if (id !== serverId) return;
      failed = !(e instanceof ApiError && e.status === 501);
    } finally {
      loading = false;
    }
  }

  $effect(() => {
    const id = serverId;
    untrack(() => {
      data = null;
      failed = false;
      plan = null;
    });
    void load(id);
    const timer = setInterval(() => void load(id), POLL_MS);
    return () => clearInterval(timer);
  });

  function badgeClass(l: FwListener): string {
    switch (l.firewall) {
      case 'allowed':
      case 'restricted':
        return 'good';
      case 'open':
        return l.risky ? 'crit' : 'warn';
      default:
        return 'mut';
    }
  }

  function badgeText(l: FwListener): string {
    if (l.firewall === 'open') return $trs(l.risky ? 'fw.ports.st.openRisky' : 'fw.ports.st.open');
    return $trs(`fw.ports.st.${l.firewall}`);
  }

  function startPlan(l: FwListener): void {
    plan = { l, mode: l.risky ? 'close' : 'restrict' };
    source = '';
    planError = null;
  }

  async function submitPlan(): Promise<void> {
    if (!plan || planBusy) return;
    const { l, mode } = plan;
    const proto = l.proto;
    const port = String(l.port);
    const texts: string[] = [];
    if (mode === 'restrict') {
      if (!source.trim()) {
        planError = $trs('fw.ports.needSource');
        return;
      }
      const allow = buildRule({ proto, port, source: source.trim(), action: 'accept', comment: '' }, listRefs);
      if (!allow.ok) {
        planError = $trs(allow.error);
        return;
      }
      texts.push(allow.text);
    }
    const deny = buildRule({ proto, port, source: '', action: 'drop', comment: '' }, listRefs);
    if (!deny.ok) {
      planError = $trs(deny.error);
      return;
    }
    texts.push(deny.text);
    planBusy = true;
    planError = await onrules(texts);
    planBusy = false;
    if (!planError) plan = null;
  }

  function addrText(l: FwListener): string {
    return l.addrs.join(', ');
  }
</script>

<section class="ports stack">
  <div class="head">
    <h3 class="sect">{$trs('fw.ports.title')}</h3>
    <button class="btn" type="button" disabled={loading} onclick={() => void load(serverId)}>{$trs('fw.ports.refresh')}</button>
  </div>

  {#if !data}
    <p class="muted small">{failed ? $trs('fw.ports.error') : $trs('common.loading')}</p>
  {:else if !data.available}
    <p class="muted small">{$trs('fw.ports.noSs')}</p>
  {:else}
    <p>
      {$trs('fw.ports.summary', { n: all.length })}
      {#if counts.norule > 0}<span class="crit">{$trs('fw.ports.exposed', { n: counts.norule })}</span>{/if}
    </p>
    <div class="row fl" class:pc-seg={$isPhone} role="group" aria-label={$trs('fw.ports.filter')}>
      <button class="chip" type="button" aria-pressed={filter === 'all'} onclick={() => (filter = 'all')}>{$trs('fw.ports.f.all')} <span class="muted">{counts.all}</span></button>
      <button class="chip" type="button" aria-pressed={filter === 'out'} onclick={() => (filter = 'out')}>{$trs('fw.ports.f.out')} <span class="muted">{counts.out}</span></button>
      <button class="chip" type="button" aria-pressed={filter === 'local'} onclick={() => (filter = 'local')}>{$trs('fw.ports.f.local')} <span class="muted">{counts.local}</span></button>
      <button class="chip" type="button" aria-pressed={filter === 'norule'} onclick={() => (filter = 'norule')}>{$trs('fw.ports.f.norule')} <span class="muted">{counts.norule}</span></button>
    </div>

    {#if $isPhone}
      {#if rows.length === 0}
        <p class="muted small">{$trs('fw.ports.none')}</p>
      {:else}
        <div class="pc-list">
          {#each rows as l (l.proto + l.port + l.scope + l.process + addrText(l))}
            <div class="pc-li" class:wn={l.firewall === 'open'}>
              <div class="pc-mn">
                <div class="pc-l1">
                  <b class="mono">{l.port}/{l.proto}</b>
                  <span class="badge {badgeClass(l)}">{badgeText(l)}</span>
                </div>
                <div class="pc-l2">{l.process || '—'}{#if l.pid} ({l.pid}){/if} · {l.proto === 'tcp' ? $trs('mobC.fw.conns', { n: l.conns }) : '—'}</div>
                <div class="pc-l2 mono">{addrText(l)}</div>
              </div>
              {#if l.firewall === 'open'}
                <button class="btn sm" type="button" onclick={() => startPlan(l)}>{$trs(l.risky ? 'fw.ports.close' : 'fw.ports.restrict')}</button>
              {/if}
            </div>
          {/each}
        </div>
      {/if}
    {:else}
    <div class="tw">
      <table class="tbl">
        <thead>
          <tr>
            <th>{$trs('fw.ports.col.port')}</th>
            <th>{$trs('fw.ports.col.addr')}</th>
            <th>{$trs('fw.ports.col.process')}</th>
            <th class="r">{$trs('fw.ports.col.conns')}</th>
            <th>{$trs('fw.ports.col.fw')}</th>
            <th class="r"><span class="sr">{$trs('fw.col.actions')}</span></th>
          </tr>
        </thead>
        <tbody>
          {#each rows as l (l.proto + l.port + l.scope + l.process + addrText(l))}
            <tr class:hl={l.firewall === 'open'}>
              <td class="mono">{l.port}/{l.proto}</td>
              <td class="mono addr">{addrText(l)}</td>
              <td>{l.process || '—'}{#if l.pid} <span class="muted small">({l.pid})</span>{/if}</td>
              <td class="r num">{l.proto === 'tcp' ? l.conns : '—'}</td>
              <td><span class="badge {badgeClass(l)}">{badgeText(l)}</span></td>
              <td class="r">
                {#if l.firewall === 'open'}
                  <button class="btn sm" type="button" onclick={() => startPlan(l)}>{$trs(l.risky ? 'fw.ports.close' : 'fw.ports.restrict')}</button>
                {/if}
              </td>
            </tr>
          {:else}
            <tr><td class="empty" colspan="6">{$trs('fw.ports.none')}</td></tr>
          {/each}
        </tbody>
      </table>
    </div>
    {/if}

    {#if plan}
      <div class="plan stack">
        <p>
          {$trs('fw.ports.plan', { port: `${plan.l.port}/${plan.l.proto}`, process: plan.l.process || '?' })}
        </p>
        <div class="row" role="group" aria-label={$trs('fw.ports.mode')}>
          <button class="chip" type="button" aria-pressed={plan.mode === 'close'} onclick={() => plan && (plan.mode = 'close')}>{$trs('fw.ports.modeClose')}</button>
          <button class="chip" type="button" aria-pressed={plan.mode === 'restrict'} onclick={() => plan && (plan.mode = 'restrict')}>{$trs('fw.ports.modeRestrict')}</button>
        </div>
        {#if plan.mode === 'restrict'}
          <label class="field">
            <span class="label">{$trs('fw.ports.source')}</span>
            <SourceInput
              cls="input mono"
              lists={lists}
              placeholder="10.20.0.0/24"
              ariaLabel={$trs('fw.ports.source')}
              bind:value={source}
              oninput={() => (planError = null)}
            />
          </label>
        {/if}
        <p class="small muted">{$trs('fw.ports.planNote')}</p>
        {#if planError}<p class="error small" role="alert">{planError}</p>{/if}
        <div class="row end">
          <button class="btn" type="button" onclick={() => (plan = null)}>{$trs('common.cancel')}</button>
          <button class="btn primary" type="button" disabled={planBusy} onclick={() => void submitPlan()}>{$trs('fw.ports.toDraft')}</button>
        </div>
      </div>
    {/if}

    <div class="grid2">
      <div class="stack">
        <h3 class="sect">{$trs('fw.ports.top')}</h3>
        {#if data.remotes.length && $isPhone}
          <div class="pc-list">
            {#each data.remotes as r (r.addr + r.port)}
              <div class="pc-li">
                <div class="pc-mn">
                  <div class="pc-l1 mono"><b>{r.addr}</b></div>
                  <div class="pc-l2">{$trs('fw.ports.col.port')} {r.port}</div>
                </div>
                <span class="pc-r">{r.conns}</span>
              </div>
            {/each}
          </div>
        {:else if data.remotes.length}
          <div class="tw">
            <table class="tbl">
              <thead><tr><th>{$trs('fw.ports.col.remote')}</th><th>{$trs('fw.ports.col.port')}</th><th class="r">{$trs('fw.ports.col.conns')}</th></tr></thead>
              <tbody>
                {#each data.remotes as r (r.addr + r.port)}
                  <tr><td class="mono">{r.addr}</td><td class="mono">{r.port}</td><td class="r num">{r.conns}</td></tr>
                {/each}
              </tbody>
            </table>
          </div>
        {:else}
          <p class="muted small">{$trs('fw.ports.noRemotes')}</p>
        {/if}
      </div>
      <div class="stack">
        <h3 class="sect">{$trs('fw.ports.howTitle')}</h3>
        <p class="small muted">{$trs('fw.ports.how')}</p>
      </div>
    </div>
  {/if}
</section>

<style>
  .ports {
    display: flex;
    flex-direction: column;
    gap: 10px;
    min-width: 0;
  }

  .head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
  }

  h3 {
    margin: 0;
  }

  .sect {
    font-family: var(--font-display);
    font-size: 17px;
    font-weight: 600;
  }

  p {
    margin: 0;
  }

  .crit {
    color: var(--crit);
  }

  .tw {
    overflow-x: auto;
    min-width: 0;
  }

  .tbl {
    width: 100%;
    border-collapse: collapse;
    font-size: 13px;
  }

  .tbl th {
    font-family: var(--font-display);
    font-size: 12px;
    font-weight: 400;
    text-transform: uppercase;
    letter-spacing: 0.08em;
    color: var(--muted);
    text-align: left;
    padding: 6px 8px;
    border-bottom: 2px solid var(--line);
    white-space: nowrap;
  }

  .tbl td {
    padding: 7px 8px;
    border-bottom: 2px dashed var(--seg-off);
    vertical-align: middle;
  }

  .tbl .r {
    text-align: right;
  }

  .tbl td.r {
    white-space: nowrap;
  }

  .tbl tr.hl td {
    background: color-mix(in srgb, var(--accent) 9%, transparent);
  }

  .addr {
    overflow-wrap: anywhere;
  }

  .num {
    font-variant-numeric: tabular-nums;
  }

  .empty {
    color: var(--muted);
  }

  .badge {
    display: inline-block;
    padding: 1px 6px;
    border: 2px solid var(--muted);
    color: var(--muted);
    font-size: 12px;
    white-space: nowrap;
  }

  .badge.good {
    border-color: var(--good);
    color: var(--good);
  }

  .badge.warn {
    border-color: var(--warn);
    color: var(--warn);
  }

  .badge.crit {
    border-color: var(--crit);
    color: var(--crit);
  }

  .tbl :global(.btn.sm) {
    display: inline-flex;
    align-items: center;
    height: 30px;
    padding-block: 0;
    font-size: 13px;
  }

  .plan {
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 12px;
    border: 2px solid var(--accent);
    background: color-mix(in srgb, var(--accent) 7%, transparent);
  }

  .plan :global(.input) {
    height: 36px;
    padding-block: 0;
  }

  .grid2 {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 20px;
  }

  .error {
    color: var(--crit);
  }

  .sr {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip: rect(0 0 0 0);
  }

  @media (max-width: 800px) {
    .grid2 {
      grid-template-columns: minmax(0, 1fr);
    }
  }

  @media (max-width: 720px) {
    .fl.pc-seg {
      gap: 0;
    }

    .fl .chip {
      justify-content: center;
      flex: 1;
      min-width: 0;
      padding: 0 2px;
      border-width: 0 0 0 2px;
      box-shadow: none;
      font-size: 12.5px;
      line-height: 1.15;
      text-align: center;
      white-space: normal;
    }

    .fl .chip:first-child {
      border-left-width: 0;
    }

    .fl .chip .muted {
      margin-left: 3px;
    }

    .pc-l1 .badge {
      font-size: 11.5px;
    }

    .plan :global(.input) {
      height: 44px;
    }

    .plan :global(.src) {
      display: flex;
      width: 100%;
    }
  }
</style>
