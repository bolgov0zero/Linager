<script lang="ts">
  import { tr } from '../../i18n';
  import type { ChartEvent, ChartPoint } from './model';

  // Custom SVG bar chart: one bar per point (average), a peak dot above it,
  // optional thin secondary bars, time markers and a hover tooltip.
  let {
    points,
    secondary = [],
    events = [],
    step,
    format,
    yMax = 'auto',
    color = 'var(--accent)',
    secondaryColor = 'var(--ink)',
    primaryLabel = '',
    secondaryLabel = '',
    tickLabel,
    fullLabel,
    label,
  }: {
    points: ChartPoint[];
    secondary?: ChartPoint[];
    events?: ChartEvent[];
    step: number;
    format: (value: number) => string;
    yMax?: number | 'auto';
    color?: string;
    secondaryColor?: string;
    primaryLabel?: string;
    secondaryLabel?: string;
    tickLabel: (ts: number) => string;
    fullLabel: (ts: number) => string;
    label: string;
  } = $props();

  const W = 600;
  const H = 200;
  const X0 = 52;
  const X1 = 594;
  const Y0 = 10;
  const Y1 = 162;

  let hover = $state<number | null>(null);

  const n = $derived(points.length);
  const slot = $derived(n > 0 ? (X1 - X0) / n : 0);
  const barW = $derived(Math.max(1, slot * 0.72));
  const thinW = $derived(Math.max(1, slot * 0.28));
  const stepSec = $derived(step > 0 ? step : 60);
  const t0 = $derived(points[0]?.ts ?? 0);

  const top = $derived.by(() => {
    if (typeof yMax === 'number') return yMax;
    let max = 0;
    for (const p of [...points, ...secondary]) max = Math.max(max, p.value, p.peak);
    return max > 0 ? max * 1.1 : 1;
  });
  const ticks = $derived([0, top / 2, top]);

  function y(v: number): number {
    return Y1 - (Math.min(Math.max(v, 0), top) / top) * (Y1 - Y0);
  }

  function cx(i: number): number {
    return X0 + slot * (i + 0.5);
  }

  function bars(list: ChartPoint[], width: number): string {
    let d = '';
    for (let i = 0; i < list.length; i++) {
      const h = Y1 - y(list[i].value);
      if (h <= 0) continue;
      const x = cx(i) - width / 2;
      d += `M${x.toFixed(2)} ${Y1}h${width.toFixed(2)}V${(Y1 - h).toFixed(2)}H${x.toFixed(2)}Z`;
    }
    return d;
  }

  function peaks(list: ChartPoint[]): string {
    let d = '';
    for (let i = 0; i < list.length; i++) {
      const x = cx(i);
      const py = y(list[i].peak);
      d += `M${(x - 1.6).toFixed(2)} ${py.toFixed(2)}a1.6 1.6 0 1 0 3.2 0a1.6 1.6 0 1 0 -3.2 0`;
    }
    return d;
  }

  const primaryPath = $derived(bars(points, barW));
  const secondaryPath = $derived(bars(secondary, thinW));
  const peakPath = $derived(peaks(points));

  // Event markers: x position inside the bar range, or null when outside it.
  const marks = $derived.by(() => {
    const out: { x: number; kind: string }[] = [];
    if (n === 0) return out;
    for (const e of events) {
      const idx = (e.ts - t0) / stepSec;
      if (idx < 0 || idx >= n) continue;
      out.push({ x: X0 + idx * slot, kind: e.kind });
    }
    return out;
  });

  const xLabels = $derived.by(() => {
    if (n === 0) return [] as number[];
    const idx = n === 1 ? [0] : [0, 0.25, 0.5, 0.75, 1].map((f) => Math.round(f * (n - 1)));
    return [...new Set(idx)];
  });

  const hoverPoint = $derived(hover !== null ? points[hover] : undefined);
  const hoverSecondary = $derived(hover !== null ? secondary[hover] : undefined);

  function onMove(event: PointerEvent): void {
    const svg = event.currentTarget as SVGSVGElement;
    const rect = svg.getBoundingClientRect();
    const x = ((event.clientX - rect.left) / rect.width) * W;
    hover = n > 0 && x >= X0 && x < X1 ? Math.min(n - 1, Math.floor((x - X0) / slot)) : null;
  }
</script>

{#if n === 0}
  <p class="empty muted">{$tr('srv.chart.noData')}</p>
{:else}
  <div class="chart">
    <svg
      viewBox="0 0 {W} {H}"
      role="img"
      aria-label={label}
      onpointermove={onMove}
      onpointerleave={() => (hover = null)}
    >
      {#each ticks as tv (tv)}
        <line class="grid" x1={X0} x2={X1} y1={y(tv)} y2={y(tv)} />
        <text class="ax" x={X0 - 6} y={y(tv) + 4} text-anchor="end">{format(tv)}</text>
      {/each}

      {#if hover !== null}
        <rect class="hl" x={X0 + hover * slot} y={Y0} width={slot} height={Y1 - Y0} />
      {/if}

      <path d={primaryPath} fill={color} />
      {#if secondary.length > 0}
        <path d={secondaryPath} fill={secondaryColor} />
      {/if}
      <path d={peakPath} class="peak" />

      <line class="base" x1={X0} x2={X1} y1={Y1} y2={Y1} />

      {#each marks as m, i (i)}
        <line class="mark" class:crit={m.kind === 'reboot'} x1={m.x} x2={m.x} y1={Y0} y2={Y1} />
        <polygon
          class="tri"
          class:crit={m.kind === 'reboot'}
          points="{m.x},{Y1 + 2} {m.x - 4},{Y1 + 10} {m.x + 4},{Y1 + 10}"
        />
      {/each}

      {#each xLabels as i (i)}
        <text
          class="ax"
          x={cx(i)}
          y={H - 4}
          text-anchor={i === 0 ? 'start' : i === n - 1 ? 'end' : 'middle'}>{tickLabel(points[i].ts)}</text
        >
      {/each}
    </svg>

    {#if hoverPoint}
      <div class="tip" class:flip={hover !== null && hover > n / 2} style:left="{(cx(hover ?? 0) / W) * 100}%">
        <strong>{fullLabel(hoverPoint.ts)}</strong>
        <span>{primaryLabel} {$tr('srv.chart.avg')} {format(hoverPoint.value)} · {$tr('srv.chart.peak')} {format(hoverPoint.peak)}</span>
        {#if hoverSecondary}
          <span>{secondaryLabel} {$tr('srv.chart.avg')} {format(hoverSecondary.value)} · {$tr('srv.chart.peak')} {format(hoverSecondary.peak)}</span>
        {/if}
      </div>
    {/if}
  </div>
{/if}

<style>
  .chart {
    position: relative;
  }

  svg {
    display: block;
    width: 100%;
    height: auto;
    overflow: visible;
    user-select: none;
  }

  .grid {
    stroke: var(--seg-off);
    stroke-width: 1;
  }

  .base {
    stroke: var(--line);
    stroke-width: 1.5;
  }

  .ax {
    fill: var(--muted);
    font-size: 12px;
    font-family: var(--font-body);
  }

  .peak {
    fill: var(--ink);
  }

  .hl {
    fill: var(--ink);
    opacity: 0.07;
  }

  .mark {
    stroke: var(--warn);
    stroke-width: 1;
    stroke-dasharray: 3 3;
  }

  .mark.crit {
    stroke: var(--crit);
  }

  .tri {
    fill: var(--warn);
  }

  .tri.crit {
    fill: var(--crit);
  }

  .tip {
    position: absolute;
    top: 0;
    transform: translateX(-50%);
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: 6px 8px;
    background: var(--ink);
    color: var(--surface);
    border: 2px solid var(--line);
    font-size: 12px;
    white-space: nowrap;
    pointer-events: none;
    z-index: 1;
  }

  .tip.flip {
    transform: translateX(-100%);
  }

  .empty {
    margin: 0;
    padding: 24px 0;
    text-align: center;
  }
</style>
