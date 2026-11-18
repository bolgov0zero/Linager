<script lang="ts">
  import { tr } from '../../i18n';
  import type { Totals } from './status';

  let { summary }: { summary: Totals } = $props();

  const cpuText = $derived(summary.cpuAvg === null ? '—' : `${Math.round(summary.cpuAvg)}%`);
</script>

<section class="tiles" aria-label={$tr('dash.summary')}>
  <div class="tile card">
    {#if summary.offline > 0}<i class="flag crit" aria-hidden="true"></i>{/if}
    <span class="label">{$tr('dash.tile.online')}</span>
    <strong class="val" class:crit={summary.offline > 0}>{summary.online}/{summary.total}</strong>
    <span class="hint">{$tr('dash.tile.onlineSub', { n: summary.offline })}</span>
  </div>

  <div class="tile card">
    {#if summary.problems > 0}<i class="flag {summary.critical > 0 ? 'crit' : 'warn'}" aria-hidden="true"></i>{/if}
    <span class="label">{$tr('dash.tile.attention')}</span>
    <strong class="val" class:crit={summary.critical > 0} class:warn={summary.critical === 0 && summary.problems > 0}
      >{summary.problems}</strong>
    <span class="hint">{$tr('dash.tile.attentionSub', { n: summary.critical })}</span>
  </div>

  <div class="tile card">
    <span class="label">{$tr('dash.tile.updates')}</span>
    <strong class="val">{summary.updates}</strong>
    <span class="hint"
      >{$tr('dash.tile.updatesSub', { n: summary.rebootPending })}{#if summary.imageUpdates > 0}{' · '}{$tr('dash.tile.imagesSub', { n: summary.imageUpdates })}{/if}</span>
  </div>

  <div class="tile card">
    <span class="label">{$tr('dash.tile.cpu')}</span>
    <strong class="val">{cpuText}</strong>
    <span class="hint">{$tr('dash.tile.cpuSub')}</span>
  </div>
</section>

<style>
  .tiles {
    display: grid;
    grid-template-columns: repeat(4, minmax(0, 1fr));
    gap: 14px;
  }

  .tile {
    position: relative;
    display: flex;
    flex-direction: column;
    gap: 4px;
    padding: 11px 14px;
    min-width: 0;
  }

  .label {
    color: var(--muted);
    font-size: 11px;
    letter-spacing: 0.06em;
    text-transform: uppercase;
  }

  .val {
    font-family: var(--font-display);
    font-variation-settings: "ELSH" 40;
    font-weight: 400;
    font-size: 31px;
    line-height: 1.1;
    margin-top: 4px;
  }

  .val.crit {
    color: var(--crit);
  }

  .val.warn {
    color: var(--warn);
  }

  .hint {
    color: var(--muted);
    font-size: 11px;
  }

  .flag {
    position: absolute;
    top: 5px;
    right: 5px;
    width: 7px;
    height: 7px;
    background: var(--crit);
  }

  .flag.warn {
    background: var(--warn);
  }

  /* Phone: one strip of four cells instead of four cards. */
  @media (max-width: 720px) {
    .tiles {
      grid-template-columns: repeat(4, minmax(0, 1fr));
      gap: 0;
      border: 2px solid var(--line);
      box-shadow: 3px 3px 0 var(--shade);
      background: var(--surface);
    }

    .tile.card {
      gap: 0;
      padding: 6px 7px 5px;
      border: 0;
      border-left: 2px solid var(--line);
      box-shadow: none;
    }

    .tile.card:first-child {
      border-left: 0;
    }

    .label {
      font-size: 10px;
      letter-spacing: 0.04em;
    }

    .val {
      margin-top: 0;
      font-size: 20px;
    }

    .hint {
      display: -webkit-box;
      overflow: hidden;
      font-size: 10px;
      line-height: 1.25;
      -webkit-box-orient: vertical;
      -webkit-line-clamp: 3;
      line-clamp: 3;
    }

    .flag {
      top: 0;
      right: 0;
      width: 6px;
      height: 6px;
    }
  }
</style>
