<script lang="ts">
  import type { ServerCard } from '../../api';
  import { tr } from '../../i18n';
  import EditBar from './EditBar.svelte';
  import { fmtAgo, fmtPct, fmtRate, type Units } from './format';
  import Tags from './Tags.svelte';
  import { levelOf, tagsOf, toneOfPct } from './status';

  let {
    card,
    now,
    editing,
    index,
    count,
    onopen,
    onmove,
    onremove,
  }: {
    card: ServerCard;
    now: number;
    editing: boolean;
    index: number;
    count: number;
    onopen: (name: string) => void;
    onmove: (dir: -1 | 1) => void;
    onremove: () => void;
  } = $props();

  const level = $derived(levelOf(card));
  const tags = $derived(tagsOf(card));
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
  <div class="line">
    <h3 class="name"><span class="sq" aria-hidden="true"></span>{card.name}</h3>
    <div class="row acts">
      <button class="btn" type="button" disabled title={$tr('dash.terminalSoon')}>{$tr('dash.terminal')}</button>
      <button class="btn" type="button" onclick={() => onopen(card.name)}>{$tr('dash.open')}</button>
    </div>
    {#if editing}
      <EditBar {index} {count} {onmove} {onremove} />
    {/if}
    <div class="stats mono">
      <span class="stat"><span class="lbl">{$tr('dash.cpu')}</span> <b class={toneOfPct(cpu)}>{fmtPct(cpu)}</b></span>
      <span class="stat"><span class="lbl">{$tr('dash.ram')}</span> <b class={toneOfPct(ram)}>{fmtPct(ram)}</b></span>
      <span class="stat"><span class="lbl">{$tr('dash.disk')}</span> <b class={toneOfPct(disk)}>{fmtPct(disk)}</b></span>
    </div>
  </div>

  <div class="line sub-line">
    <span class="addr mono small muted">{card.address} · {card.os} · {$tr('dash.agent')} {card.agent_version}</span>
    <Tags {tags} />
    <span class="net mono small">
      {#if card.online}
        ↓ {fmtRate(sample?.rx_bps)} &nbsp;↑ {fmtRate(sample?.tx_bps)}
      {:else}
        <span class="muted">{offlineText}</span>
      {/if}
    </span>
  </div>
</article>

<style>
  .row-card {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 10px 14px;
    background: var(--surface);
    border: 2px solid var(--line);
    border-left: 6px solid var(--strip);
    box-shadow: 3px 3px 0 var(--shade);
    min-width: 0;
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

  .line {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px 12px;
  }

  .name {
    display: flex;
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
    gap: 6px;
  }

  .acts .btn {
    padding: 3px 10px;
    font-size: 13px;
  }

  .stats {
    display: flex;
    flex-wrap: wrap;
    gap: 14px;
    margin-left: auto;
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

  .sub-line {
    padding-left: 22px;
  }

  .addr {
    overflow-wrap: anywhere;
  }

  .net {
    margin-left: auto;
    white-space: nowrap;
  }

  .off .stat b {
    color: var(--muted);
  }

  @media (max-width: 560px) {
    .sub-line {
      padding-left: 0;
    }

    .stats {
      margin-left: 0;
      width: 100%;
    }

    .net {
      margin-left: 0;
    }
  }
</style>
