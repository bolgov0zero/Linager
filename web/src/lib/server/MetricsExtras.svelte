<script lang="ts">
  import type { HistoryEvent, Period } from '../../api';
  import { locale, tr } from '../../i18n';
  import { formatDateTime, formatStep } from './format';

  let { events }: { events: HistoryEvent[] } = $props();

  // Retention policy from the UI spec: precision per period.
  const POLICY: { periods: Period[]; step: number }[] = [
    { periods: ['1h'], step: 5 },
    { periods: ['24h', '7d'], step: 60 },
    { periods: ['30d', '90d'], step: 300 },
    { periods: ['180d'], step: 900 },
    { periods: ['360d'], step: 3600 },
  ];
</script>

<div class="extras">
  <section class="card">
    <h3>{$tr('srv.events.title')}</h3>
    {#if events.length === 0}
      <p class="small muted">{$tr('srv.events.none')}</p>
    {:else}
      <ul class="events">
        {#each events as ev, i (i)}
          <li>
            <time class="mono">{formatDateTime(ev.ts, $locale)}</time>
            <span class="tag" class:crit={ev.kind === 'reboot'}>{$tr(`srv.event.${ev.kind}`)}</span>
            <span class="msg">{ev.message}</span>
          </li>
        {/each}
      </ul>
    {/if}
    <p class="hint">{$tr('srv.events.legend')}</p>
  </section>

  <section class="card">
    <h3>{$tr('srv.policy.title')}</h3>
    <div class="grid-table" role="table">
      <div class="grow head" role="row">
        <span role="columnheader">{$tr('srv.policy.period')}</span>
        <span role="columnheader">{$tr('srv.policy.precision')}</span>
      </div>
      {#each POLICY as row (row.step)}
        <div class="grow" role="row">
          <span role="cell">{row.periods.map((p) => $tr(`srv.period.${p}`)).join(', ')}</span>
          <span role="cell">{formatStep(row.step, $tr)}</span>
        </div>
      {/each}
    </div>
    <p class="hint">{$tr('srv.policy.note')}</p>
  </section>
</div>

<style>
  .extras {
    display: grid;
    gap: 16px;
    grid-template-columns: 1fr;
  }

  @media (min-width: 760px) {
    .extras {
      grid-template-columns: 1fr 1fr;
    }
  }

  .extras section {
    display: flex;
    flex-direction: column;
    gap: 12px;
    min-width: 0;
  }

  h3 {
    margin: 0;
    font-size: 16px;
    color: var(--muted);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }

  /* The list scrolls inside the block, so a long history does not stretch it. */
  .events {
    list-style: none;
    margin: 0;
    padding: 0 6px 0 0;
    max-height: 320px;
    overflow-y: auto;
  }

  .events li {
    display: grid;
    grid-template-columns: auto auto;
    gap: 4px 10px;
    align-items: center;
    padding: 8px 0;
    border-bottom: 1px dashed var(--line);
  }

  .events .msg {
    grid-column: 1 / -1;
    overflow-wrap: anywhere;
  }

  .tag {
    justify-self: start;
    padding: 0 6px;
    border: 2px solid var(--warn);
    color: var(--warn);
    font-size: 12px;
    white-space: nowrap;
  }

  .tag.crit {
    border-color: var(--crit);
    color: var(--crit);
  }

  .grid-table {
    display: flex;
    flex-direction: column;
  }

  .grow {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 8px;
    padding: 8px;
    border-bottom: 1px dashed var(--line);
  }

  .grow.head {
    color: var(--muted);
    border-bottom: 2px solid var(--line);
  }

  .hint {
    margin: 0;
    color: var(--muted);
    font-size: 12px;
  }
</style>
