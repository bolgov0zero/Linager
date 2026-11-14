<script lang="ts">
  import type { ServerCard } from '../../api';
  import { tr } from '../../i18n';
  import { openTermWindow } from '../terminal/windows';
  import EditBar from './EditBar.svelte';
  import { fmtAgo, fmtPct, fmtRate, type Units } from './format';
  import Tags from './Tags.svelte';
  import { levelOf, tagsOf, toneOfPct, type AgentFlag } from './status';

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
  const offlineText = $derived.by(() => {
    if (card.online) return '';
    const ago = fmtAgo(card.last_seen_at, now, units);
    return ago ? $tr('dash.offlineFor', { v: ago }) : $tr('dash.offline');
  });
</script>

<article class="row-card {level}">
  <div class="head">
    <h3 class="name"><span class="sq" aria-hidden="true"></span>{card.name}</h3>
  </div>
  <div class="row acts">
    <button class="btn" type="button" disabled={!card.online} onclick={() => openTermWindow(card.id, card.name)}>{$tr('dash.terminal')}</button>
    <button class="btn" type="button" onclick={() => onopen(card.name)}>{$tr('dash.open')}</button>
  </div>
  <div class="metrics">
    <div class="stats mono">
      <span class="stat"><span class="lbl">{$tr('dash.cpu')}</span> <b class={toneOfPct(cpu)}>{fmtPct(cpu)}</b></span>
      <span class="stat"><span class="lbl">{$tr('dash.ram')}</span> <b class={toneOfPct(ram)}>{fmtPct(ram)}</b></span>
      <span class="stat"><span class="lbl">{$tr('dash.disk')}</span> <b class={toneOfPct(disk)}>{fmtPct(disk)}</b></span>
    </div>
    {#if editing}
      <EditBar {index} {count} {onmove} {onremove} />
    {/if}
  </div>

  <span class="addr mono small muted">{card.address} · {card.os} · {$tr('dash.agent')} {card.agent_version}</span>
  <div class="tagbox">
    <Tags {tags} />
  </div>
  <span class="net mono small">
    {#if card.online}
      ↓ {fmtRate(sample?.rx_bps)} &nbsp;↑ {fmtRate(sample?.tx_bps)}
    {:else}
      <span class="muted">{offlineText}</span>
    {/if}
  </span>
</article>

<style>
  /* Two lines per server; the buttons stand one above the other to the right of the metrics:
     name     | tags | metrics | Terminal
     address (spans)  | network | Open */
  .row-card {
    display: grid;
    grid-template-columns: 250px minmax(0, 1fr) auto auto;
    grid-template-areas:
      'head tags metrics acts'
      'addr addr net acts';
    column-gap: 16px;
    row-gap: 4px;
    align-items: center;
    padding: 9px 14px 9px 18px;
    background: var(--surface);
    border: 2px solid var(--line);
    border-left: 6px solid var(--strip);
    box-shadow: 3px 3px 0 var(--shade);
    min-width: 0;
  }

  .head {
    grid-area: head;
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
  }

  .acts {
    grid-area: acts;
  }

  .tagbox {
    grid-area: tags;
    min-width: 0;
  }

  .addr {
    grid-area: addr;
  }

  .net {
    grid-area: net;
  }

  .row-card.ok {
    --strip: var(--ink);
    --sq: var(--good);
  }

  .row-card.warn {
    --strip: var(--warn);
    --sq: var(--warn);
  }

  .row-card.crit {
    --strip: var(--crit);
    --sq: var(--crit);
  }

  .row-card.off {
    --strip: var(--muted);
    --sq: var(--muted);
    color: var(--muted);
  }

  .name {
    display: flex;
    font-variation-settings: "ELSH" 40;
    align-items: center;
    gap: 10px;
    margin: 0;
    font-size: 18px;
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

  .acts {
    flex-direction: column;
    align-items: stretch;
    gap: 6px;
  }

  .acts .btn {
    padding: 3px 10px;
    font-size: 13px;
  }

  .metrics {
    grid-area: metrics;
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: flex-end;
    gap: 8px 14px;
  }

  .stats {
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: 14px;
  }

  .stat b {
    font-weight: 400;
    font-size: 15px;
  }

  .lbl {
    color: var(--muted);
    font-size: 12px;
  }

  .stat b.crit {
    color: var(--crit);
  }

  .stat b.warn {
    color: var(--warn);
  }

  .addr {
    overflow-wrap: anywhere;
  }

  .net {
    justify-self: end;
    white-space: nowrap;
  }

  .off .stat b {
    color: var(--muted);
  }

  /* Narrow screens: one column, four lines per server. */
  @media (max-width: 560px) {
    .row-card {
      grid-template-columns: minmax(0, 1fr) auto;
      grid-template-areas:
        'head acts'
        'addr acts'
        'metrics metrics'
        'tags net';
      padding: 9px 12px;
      row-gap: 6px;
    }

    .metrics {
      justify-content: flex-start;
    }

    .stats {
      justify-content: flex-start;
    }
  }
</style>
