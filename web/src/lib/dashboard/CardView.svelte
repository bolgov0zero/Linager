<script lang="ts">
  import PixIcon from '../ui/PixIcon.svelte';
  import type { ServerCard } from '../../api';
  import { tr } from '../../i18n';
  import { openAssistantWindow, openTermWindow } from '../terminal/windows';
  import CardIcon from './CardIcon.svelte';
  import Segments from '../ui/Segments.svelte';
  import Spark from '../ui/Spark.svelte';
  import EditBar from './EditBar.svelte';
  import { fmtAgo, fmtPct, fmtRate, fmtUptime, type Units } from './format';
  import OsBadge from './OsBadge.svelte';
  import Tags from './Tags.svelte';
  import { isPhone } from '../ui/viewport';
  import { levelOf, osKind, tagsOf, type AgentFlag } from './status';

  let {
    card,
    agent = null,
    now,
    editing,
    index,
    count,
    onopen,
    onmove,
    onremove,
    ondelete,
  }: {
    card: ServerCard;
    /** Agent update state from the updates API. */
    agent?: AgentFlag;
    now: number;
    editing: boolean;
    index: number;
    count: number;
    onopen: (name: string, tab?: string) => void;
    onmove: (dir: -1 | 1) => void;
    onremove: () => void;
    /** Delete the server from the admin panel altogether (the owner asks for confirmation). */
    ondelete: () => void;
  } = $props();

  const level = $derived(levelOf(card));
  const tags = $derived(tagsOf(card, agent));
  const sample = $derived(card.online ? card.sample : null);
  const cpu = $derived(sample ? sample.cpu : null);
  const ram = $derived(sample ? sample.mem_pct : null);
  const disk = $derived(sample ? sample.disk_pct : null);
  const units = $derived<Units>({ d: $tr('dash.unit.d'), h: $tr('dash.unit.h'), min: $tr('dash.unit.min') });
  const footText = $derived.by(() => {
    if (card.online) return sample ? $tr('dash.uptime', { v: fmtUptime(sample.uptime, units) }) : '—';
    const ago = fmtAgo(card.last_seen_at, now, units);
    return ago ? $tr('dash.offlineFor', { v: ago }) : $tr('dash.offline');
  });
</script>

{#if $isPhone && !editing}
  <!-- Phone: the whole card opens the server, the terminal opens its tab; the rest is shorter than on a desktop. -->
  <article class="card srv pc {level}">
    <button class="hit" type="button" aria-label={card.name} onclick={() => onopen(card.name)}></button>
    <div class="head">
      <h3 class="name"><span class="sq" aria-hidden="true"></span>{card.name}</h3>
      <OsBadge os={card.os} />
      <button class="btn ic term" type="button" title={$tr('dash.terminal')} aria-label={$tr('dash.terminal')} disabled={!card.online} onclick={() => onopen(card.name, 'terminal')}><CardIcon name="terminal" /></button>
    </div>
    <p class="sub mono muted small">{card.address} · {card.os} · {$tr('dash.agent')} {card.agent_version}</p>
    {#if card.online}
      <div class="pm">
        {#each [['dash.cpu', cpu], ['dash.ram', ram], ['dash.disk', disk]] as [key, v] (key)}
          <div>
            <span class="pl"><span class="lbl">{$tr(String(key))}</span><span class="mono">{fmtPct(v as number | null)}</span></span>
            <Segments value={v as number | null} segments={10} />
          </div>
        {/each}
      </div>
    {:else}
      <p class="muted small mono off-note">{footText}</p>
    {/if}
    <div class="pft">
      <Tags {tags} />
      {#if card.online}<span class="mono small muted net2"><PixIcon name="arrow-down" size={12} /> {fmtRate(sample?.rx_bps)} <PixIcon name="arrow-up" size={12} /> {fmtRate(sample?.tx_bps)}</span>{/if}
    </div>
  </article>
{:else}
<article class="card srv {level}">
  <div class="head">
    <h3 class="name"><span class="sq" aria-hidden="true"></span>{card.name}</h3>
    <OsBadge os={card.os} pushed={!editing} />
    {#if editing}
      <EditBar {index} {count} {onmove} {onremove} />
    {/if}
  </div>

  <p class="sub mono muted small">{card.address} · {card.os} · {$tr('dash.agent')} {card.agent_version}</p>
  <Tags {tags} />

  <div class="body">
    <div class="meters">
      <div class="meter">
        <span class="lbl">{$tr('dash.cpu')}</span>
        <Segments value={cpu} />
        <span class="val mono">{fmtPct(cpu)}</span>
      </div>
      <div class="meter">
        <span class="lbl">{$tr('dash.ram')}</span>
        <Segments value={ram} />
        <span class="val mono">{fmtPct(ram)}</span>
      </div>
      <div class="meter">
        <span class="lbl">{$tr('dash.disk')}</span>
        <Segments value={disk} />
        <span class="val mono">{fmtPct(disk)}</span>
      </div>
    </div>

    <div class="chart">
      <Spark values={card.online ? card.spark : []} height={24} />
    </div>
    <div class="net mono small">
      <span><PixIcon name="arrow-down" size={12} /> {fmtRate(sample?.rx_bps)}</span>
      <span><PixIcon name="arrow-up" size={12} /> {fmtRate(sample?.tx_bps)}</span>
      <span class="cap">{$tr('dash.cpu1m')}</span>
    </div>
  </div>

  <footer class="foot">
    <span class="small mono muted">{footText}</span>
    <div class="row end">
      <button class="btn ic" type="button" title={$tr('dash.terminal')} aria-label={$tr('dash.terminal')} disabled={!card.online} onclick={() => openTermWindow(card.id, card.name)}><CardIcon name="terminal" /></button>
      {#if osKind(card.os) === 'windows'}
        <button class="btn ic assist" type="button" title={$tr('dash.assistant')} aria-label={$tr('dash.assistant')} disabled={!card.online} onclick={() => openAssistantWindow(card.id, card.name)}><CardIcon name="assistant" /></button>
      {/if}
      <button class="btn ic" type="button" title={$tr('dash.open')} aria-label={$tr('dash.open')} onclick={() => onopen(card.name)}><CardIcon name="open" /></button>
      <button class="btn ic del" type="button" title={$tr('dash.deleteServer')} aria-label={$tr('dash.deleteServer')} onclick={ondelete}><CardIcon name="trash" /></button>
    </div>
  </footer>
</article>
{/if}

<style>
  .srv {
    flex: 1;
    display: flex;
    flex-direction: column;
    padding: 13px 14px 0;
    border-top-width: 6px;
    border-top-color: var(--strip);
    min-width: 0;
  }

  .srv.ok {
    --strip: var(--ink);
    --sq: var(--good);
  }

  .srv.warn {
    --strip: var(--warn);
    --sq: var(--warn);
  }

  .srv.crit {
    --strip: var(--crit);
    --sq: var(--crit);
  }

  .srv.off {
    --strip: var(--muted);
    --sq: var(--muted);
  }

  .head {
    display: flex;
    align-items: center;
    gap: 7px;
    min-height: 29px;
  }

  .name {
    display: flex;
    font-variation-settings: "ELSH" 40;
    align-items: center;
    gap: 9px;
    margin: 0;
    font-size: 18px;
    line-height: 1.1;
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .sq {
    flex: none;
    width: 11px;
    height: 11px;
    background: var(--sq);
  }

  .sub {
    margin: 5px 0 9px;
    overflow-wrap: anywhere;
  }

  .body {
    margin-top: 9px;
  }

  .off .body {
    opacity: 0.55;
  }

  .meters {
    display: flex;
    flex-direction: column;
    gap: 5px;
  }

  .meter {
    display: grid;
    grid-template-columns: 36px minmax(0, 1fr) 38px;
    align-items: center;
    gap: 9px;
  }

  .lbl {
    color: var(--muted);
    font-size: 11px;
  }

  .val {
    text-align: right;
  }

  .chart {
    margin-top: 11px;
    border-bottom: 2px dashed var(--seg-off);
    padding-bottom: 5px;
  }

  .net {
    display: flex;
    flex-wrap: wrap;
    justify-content: space-between;
    gap: 4px 9px;
    margin: 5px 0 11px;
  }

  .cap {
    color: var(--muted);
  }

  .foot {
    margin: auto -14px 0;
    padding: 9px 14px 11px;
    border-top: 2px solid var(--line);
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 7px;
  }

  .foot .btn {
    padding: 4px 9px;
    font-size: 12px;
  }

  /* Icon buttons: the same height as text buttons, square-ish. */
  .foot .btn.ic {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    padding: 4px 7px;
  }

  /* The Assistant is the one action of a Windows card that is set apart by the accent colour. */
  .foot .btn.assist:not(:disabled) {
    color: var(--accent);
    border-color: var(--accent);
  }

  .foot .btn.del:hover:not(:disabled) {
    color: var(--crit);
    border-color: var(--crit);
  }

  /* Phone, edit mode (the plain card keeps the desktop structure): no chart, tighter blocks. */
  @media (max-width: 720px) {
    .srv:not(.pc) .chart,
    .srv:not(.pc) .cap {
      display: none;
    }

    .srv:not(.pc) {
      padding: 9px 11px 0;
    }

    .srv:not(.pc) .foot {
      margin: auto -11px 0;
      padding: 7px 11px 9px;
    }

    .srv:not(.pc) .foot .btn.ic {
      width: 44px;
      padding: 0;
    }
  }

  /* Phone card */
  .pc {
    position: relative;
    padding: 9px 11px;
    border-top-width: 2px;
    border-left: 5px solid var(--strip);
  }

  .pc.ok {
    --strip: var(--good);
  }

  .pc .hit {
    position: absolute;
    inset: 0;
    z-index: 1;
    padding: 0;
    background: none;
    border: 0;
    cursor: pointer;
  }

  .pc .head {
    gap: 7px;
  }

  .pc .name {
    flex: 1;
    font-size: 19px;
  }

  .pc .term {
    position: relative;
    z-index: 2;
    width: 44px;
    padding: 0;
  }

  .pc .sub {
    margin: 0 0 7px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .pm {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: 11px;
  }

  .pl {
    display: flex;
    justify-content: space-between;
    margin-bottom: 4px;
    font-size: 11px;
  }

  .pm :global(i) {
    height: 7px;
  }

  .pft {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 5px 7px;
    margin-top: 7px;
  }

  .net2 {
    margin-left: auto;
  }

  .off-note {
    margin: 4px 0 0;
  }
</style>
