<script lang="ts">
  import { api, type History, type HistoryPoint, type MetricStats, type Period } from '../../api';
  import { locale, tr } from '../../i18n';
  import TimeChart from './TimeChart.svelte';
  import MetricsExtras from './MetricsExtras.svelte';
  import { formatDateTime, formatPct, formatRate, formatStep, formatTick } from './format';
  import type { ChartPoint } from './model';

  let { serverId }: { serverId: string } = $props();

  const PERIODS: Period[] = ['1h', '24h', '7d', '30d', '90d', '180d', '360d'];

  type AvgKey = 'cpu_avg' | 'mem_avg' | 'disk_avg' | 'rx_avg' | 'tx_avg';
  type PeakKey = 'cpu_max' | 'mem_max' | 'disk_max' | 'rx_max' | 'tx_max';

  const SYNC_KEY = 'linager.metrics.sync';

  function readSync(): boolean {
    try {
      return localStorage.getItem(SYNC_KEY) === '1';
    } catch {
      return false; // storage unavailable: the default
    }
  }

  function setSync(on: boolean): void {
    sync = on;
    syncTs = null;
    try {
      localStorage.setItem(SYNC_KEY, on ? '1' : '0');
    } catch {
      // storage unavailable: the choice is not remembered
    }
  }

  let period = $state<Period>('24h');
  /** Hover of one chart is shown on all of them. */
  let sync = $state(readSync());
  let syncTs = $state<number | null>(null);
  let data = $state<History | null>(null);
  let failed = $state(false);
  let attempt = $state(0);

  // Reloads whenever the server, the period or the retry counter changes.
  $effect(() => {
    const id = serverId;
    const p = period;
    void attempt;
    let active = true;
    data = null;
    failed = false;
    syncTs = null;
    api
      .history(id, p)
      .then((h) => {
        if (active) data = h;
      })
      .catch(() => {
        if (active) failed = true;
      });
    return () => {
      active = false;
    };
  });

  function series(points: HistoryPoint[], avg: AvgKey, peak: PeakKey): ChartPoint[] {
    return points.map((p) => ({ ts: p.ts, value: p[avg], peak: p[peak] }));
  }

  const cpu = $derived(data ? series(data.points, 'cpu_avg', 'cpu_max') : []);
  const mem = $derived(data ? series(data.points, 'mem_avg', 'mem_max') : []);
  const disk = $derived(data ? series(data.points, 'disk_avg', 'disk_max') : []);
  const rx = $derived(data ? series(data.points, 'rx_avg', 'rx_max') : []);
  const tx = $derived(data ? series(data.points, 'tx_avg', 'tx_max') : []);
</script>

{#snippet statLine(s: MetricStats, fmt: (v: number) => string, prefix = '')}
  <p class="stats small">
    {#if prefix}<span class="muted">{prefix}</span>{/if}
    <span>{$tr('srv.stat.min')} <b>{fmt(s.min)}</b></span>
    <span>{$tr('srv.stat.avg')} <b>{fmt(s.avg)}</b></span>
    <span>{$tr('srv.stat.p95')} <b>{fmt(s.p95)}</b></span>
    <span>{$tr('srv.stat.peak')} <b>{fmt(s.max)}</b></span>
  </p>
{/snippet}

<div class="metrics">
  <div class="toolbar">
    <div class="row">
      {#each PERIODS as p (p)}
        <button class="chip" type="button" aria-pressed={period === p} onclick={() => (period = p)}>
          {$tr(`srv.period.${p}`)}
        </button>
      {/each}
      <label class="check sync">
        <input type="checkbox" checked={sync} onchange={(e) => setSync(e.currentTarget.checked)} />
        <span>{$tr('srv.metrics.sync')}</span>
      </label>
    </div>
    <div class="row end">
      {#if data}
        <span class="small muted">{$tr('srv.metrics.precision', { step: formatStep(data.step_seconds, $tr) })}</span>
      {/if}
      <a class="btn" href={api.historyCsvUrl(serverId, period)} download>{$tr('srv.metrics.csv')}</a>
    </div>
  </div>

  {#if failed}
    <div class="card state">
      <p class="error" role="alert">{$tr('srv.metrics.failed')}</p>
      <div>
        <button class="btn" type="button" onclick={() => attempt++}>{$tr('srv.retry')}</button>
      </div>
    </div>
  {:else if data}
    {@const h = data}
    <div class="charts">
      <article class="card chart-card">
        <h3>{$tr('srv.metrics.cpu')}</h3>
        <TimeChart
          series={[{ label: $tr('srv.metrics.cpu'), color: 'var(--accent)', points: cpu }]}
          from={h.from}
          to={h.to}
          step={h.step_seconds}
          events={h.events}
          syncTs={sync ? syncTs : null}
          onhover={sync ? (ts) => (syncTs = ts) : undefined}
          tickLabel={(ts) => formatTick(ts, period, $locale)}
          fullLabel={(ts) => formatDateTime(ts, $locale)}
          format={formatPct}
          yMax={100}
          label={$tr('srv.metrics.cpu')}
        />
        {@render statLine(h.stats.cpu, formatPct)}
      </article>

      <article class="card chart-card">
        <h3>{$tr('srv.metrics.ram')}</h3>
        <TimeChart
          series={[{ label: $tr('srv.metrics.ram'), color: 'var(--accent)', points: mem }]}
          from={h.from}
          to={h.to}
          step={h.step_seconds}
          events={h.events}
          syncTs={sync ? syncTs : null}
          onhover={sync ? (ts) => (syncTs = ts) : undefined}
          tickLabel={(ts) => formatTick(ts, period, $locale)}
          fullLabel={(ts) => formatDateTime(ts, $locale)}
          format={formatPct}
          yMax={100}
          label={$tr('srv.metrics.ram')}
        />
        {@render statLine(h.stats.mem, formatPct)}
      </article>

      <article class="card chart-card">
        <h3>{$tr('srv.metrics.disk')}</h3>
        <TimeChart
          series={[{ label: $tr('srv.metrics.disk'), color: 'var(--accent)', points: disk }]}
          from={h.from}
          to={h.to}
          step={h.step_seconds}
          events={h.events}
          syncTs={sync ? syncTs : null}
          onhover={sync ? (ts) => (syncTs = ts) : undefined}
          tickLabel={(ts) => formatTick(ts, period, $locale)}
          fullLabel={(ts) => formatDateTime(ts, $locale)}
          format={formatPct}
          yMax={100}
          label={$tr('srv.metrics.disk')}
        />
        {@render statLine(h.stats.disk, formatPct)}
      </article>

      <article class="card chart-card">
        <h3>{$tr('srv.metrics.net')}</h3>
        <TimeChart
          series={[
            { label: `↓ ${$tr('srv.chart.rx')}`, color: 'var(--series-1)', points: rx },
            { label: `↑ ${$tr('srv.chart.tx')}`, color: 'var(--series-2)', points: tx },
          ]}
          from={h.from}
          to={h.to}
          step={h.step_seconds}
          events={h.events}
          syncTs={sync ? syncTs : null}
          onhover={sync ? (ts) => (syncTs = ts) : undefined}
          tickLabel={(ts) => formatTick(ts, period, $locale)}
          fullLabel={(ts) => formatDateTime(ts, $locale)}
          format={formatRate}
          binary
          minTop={1024}
          label={$tr('srv.metrics.net')}
        />
        {@render statLine(h.stats.rx, formatRate, '↓')}
        {@render statLine(h.stats.tx, formatRate, '↑')}
      </article>
    </div>

    <MetricsExtras events={h.events} />
  {:else}
    <p class="muted">{$tr('srv.loading')}</p>
  {/if}
</div>

<style>
  .metrics {
    display: flex;
    flex-direction: column;
    gap: 16px;
  }

  .toolbar {
    display: flex;
    flex-wrap: wrap;
    justify-content: space-between;
    align-items: center;
    gap: 12px;
  }

  .toolbar .row {
    gap: 6px;
  }

  .sync {
    margin-left: 8px;
  }

  .charts {
    display: grid;
    gap: 16px;
    grid-template-columns: 1fr;
  }

  @media (min-width: 760px) {
    .charts {
      grid-template-columns: 1fr 1fr;
    }
  }

  .chart-card {
    display: flex;
    flex-direction: column;
    gap: 10px;
    min-width: 0;
  }

  .chart-card h3 {
    margin: 0;
    font-size: 16px;
    color: var(--muted);
  }

  .stats {
    display: flex;
    flex-wrap: wrap;
    gap: 4px 12px;
    margin: 0;
    color: var(--muted);
  }

  .stats b {
    color: var(--ink);
    font-weight: 400;
  }

  .state {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 12px;
  }
</style>
