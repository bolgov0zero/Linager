<script lang="ts">
  import type { ServerCard } from '../../api';
  import { tr } from '../../i18n';
  import Segments from '../ui/Segments.svelte';
  import Spark from '../ui/Spark.svelte';
  import EditBar from './EditBar.svelte';
  import { fmtAgo, fmtPct, fmtRate, fmtUptime, type Units } from './format';
  import Tags from './Tags.svelte';
  import { levelOf, tagsOf, type AgentFlag } from './status';

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

<article class="card srv {level}">
  <div class="head">
    <h3 class="name"><span class="sq" aria-hidden="true"></span>{card.name}</h3>
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
      <span>↓ {fmtRate(sample?.rx_bps)}</span>
      <span>↑ {fmtRate(sample?.tx_bps)}</span>
      <span class="cap">{$tr('dash.cpu1m')}</span>
    </div>
  </div>

  <footer class="foot">
    <span class="small mono muted">{footText}</span>
    <div class="row end">
      <button class="btn" type="button" disabled={!card.online} onclick={() => onopen(card.name, 'terminal')}>{$tr('dash.terminal')}</button>
      <button class="btn" type="button" onclick={() => onopen(card.name)}>{$tr('dash.open')}</button>
    </div>
  </footer>
</article>

<style>
  .srv {
    flex: 1;
    display: flex;
    flex-direction: column;
    padding: 14px 16px 0;
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
    gap: 8px;
    min-height: 32px;
  }

  .name {
    display: flex;
    font-variation-settings: "ELSH" 40;
    align-items: center;
    gap: 10px;
    margin: 0;
    font-size: 20px;
    line-height: 1.1;
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .sq {
    flex: none;
    width: 12px;
    height: 12px;
    background: var(--sq);
  }

  .sub {
    margin: 6px 0 10px;
    overflow-wrap: anywhere;
  }

  .body {
    margin-top: 10px;
  }

  .off .body {
    opacity: 0.55;
  }

  .meters {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .meter {
    display: grid;
    grid-template-columns: 40px minmax(0, 1fr) 42px;
    align-items: center;
    gap: 10px;
  }

  .lbl {
    color: var(--muted);
    font-size: 12px;
  }

  .val {
    text-align: right;
  }

  .chart {
    margin-top: 12px;
    border-bottom: 2px dashed var(--seg-off);
    padding-bottom: 6px;
  }

  .net {
    display: flex;
    flex-wrap: wrap;
    justify-content: space-between;
    gap: 4px 10px;
    margin: 6px 0 12px;
  }

  .cap {
    color: var(--muted);
  }

  .foot {
    margin: auto -16px 0;
    padding: 10px 16px 12px;
    border-top: 2px solid var(--line);
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
  }

  .foot .btn {
    padding: 4px 10px;
    font-size: 13px;
  }
</style>
