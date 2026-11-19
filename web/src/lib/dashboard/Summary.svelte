<script lang="ts">
  import { tr } from '../../i18n';
  import { isPhone } from '../ui/viewport';
  import type { Totals } from './status';

  /** progress: 0 = full tiles, 1 = the thin strip; it follows the scroll of the server list (wide screens only, a phone has its own strip). */
  let { summary, progress = 0 }: { summary: Totals; progress?: number } = $props();

  // The natural height of every hint (it folds away as the tiles shrink).
  let hh = $state<number[]>([0, 0, 0, 0]);
  const hv = (i: number): string | undefined => (hh[i] > 0 ? `${hh[i]}px` : undefined);

  const cpuText = $derived(summary.cpuAvg === null ? '—' : `${Math.round(summary.cpuAvg)}%`);
</script>

<section class="tiles" style:--p={$isPhone ? 0 : progress} aria-label={$tr('dash.summary')}>
  <div class="tile card">
    {#if summary.offline > 0}<i class="flag crit" aria-hidden="true"></i>{/if}
    <span class="label">{$tr('dash.tile.online')}</span>
    <strong class="val" class:crit={summary.offline > 0}>{summary.online}/{summary.total}</strong>
    <div class="hw" style:--hh={hv(0)}><span class="hint" bind:clientHeight={hh[0]}>{$tr('dash.tile.onlineSub', { n: summary.offline })}</span></div>
  </div>

  <div class="tile card">
    {#if summary.problems > 0}<i class="flag {summary.critical > 0 ? 'crit' : 'warn'}" aria-hidden="true"></i>{/if}
    <span class="label">{$tr('dash.tile.attention')}</span>
    <strong class="val" class:crit={summary.critical > 0} class:warn={summary.critical === 0 && summary.problems > 0}
      >{summary.problems}</strong>
    <div class="hw" style:--hh={hv(1)}><span class="hint" bind:clientHeight={hh[1]}>{$tr('dash.tile.attentionSub', { n: summary.critical })}</span></div>
  </div>

  <div class="tile card">
    <span class="label">{$tr('dash.tile.updates')}</span>
    <strong class="val">{summary.updates}</strong>
    <div class="hw" style:--hh={hv(2)}><span class="hint" bind:clientHeight={hh[2]}
      >{$tr('dash.tile.updatesSub', { n: summary.rebootPending })}{#if summary.imageUpdates > 0}{' · '}{$tr('dash.tile.imagesSub', { n: summary.imageUpdates })}{/if}</span></div>
  </div>

  <div class="tile card">
    <span class="label">{$tr('dash.tile.cpu')}</span>
    <strong class="val">{cpuText}</strong>
    <div class="hw" style:--hh={hv(3)}><span class="hint" bind:clientHeight={hh[3]}>{$tr('dash.tile.cpuSub')}</span></div>
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
    display: block;
    color: var(--muted);
    font-size: 11px;
  }

  /* Wide screens: --p (0..1) is the progress of the shrinking, tied to the scroll of the list. */
  @media (min-width: 721px) {
    .tiles {
      transition: --p 0.12s linear; /* the registered number property (app.css): smooths the steps of a mouse wheel */
    }

    .tiles .tile {
      gap: calc(4px * (1 - var(--p, 0)));
      padding-block: calc(11px - 6px * var(--p, 0));
    }

    .tiles .val {
      margin-top: calc(4px * (1 - var(--p, 0)));
      font-size: calc(31px - 13px * var(--p, 0));
    }

    .hw {
      max-height: calc(var(--hh, 99px) * (1 - var(--p, 0)));
      overflow: hidden;
      opacity: calc(1 - var(--p, 0));
    }

    .tiles .flag {
      top: calc(5px - 2px * var(--p, 0));
      right: calc(5px - 2px * var(--p, 0));
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .tiles {
      transition: none;
    }
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
