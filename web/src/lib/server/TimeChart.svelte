<script lang="ts" module>
  import type { ChartPoint } from './model';

  export interface Series {
    label: string;
    color: string;
    points: ChartPoint[];
  }
</script>

<script lang="ts">
  import { tr } from '../../i18n';
  import { isPhone } from '../ui/viewport';
  import type { ChartEvent } from './model';

  // Time-series chart for the metrics tab. The x axis always covers the whole selected period
  // (from..to), so a short history reads as "data only since 14:40" instead of a few stretched bars.
  // Each series is drawn as its average (line + light area); a single series also shows its peak as a
  // thin dashed line. Gaps (agent offline) break the line. Events sit as markers under the axis, only
  // reboots draw a full-height line. Hover shows a crosshair and a tooltip with the values and events.
  let {
    series,
    from,
    to,
    step,
    events = [],
    format,
    yMax = 'auto',
    minTop = 1,
    binary = false,
    tickLabel,
    fullLabel,
    label,
    syncTs = null,
    onhover,
  }: {
    series: Series[];
    from: number;
    to: number;
    step: number;
    events?: ChartEvent[];
    format: (value: number) => string;
    /** Fixed top of the y axis (e.g. 100 for percent) or 'auto'. */
    yMax?: number | 'auto';
    /** Smallest automatic top, so an idle series does not fill the chart with noise. */
    minTop?: number;
    /** Values are bytes: ticks use 1024-based round numbers. */
    binary?: boolean;
    tickLabel: (ts: number) => string;
    fullLabel: (ts: number) => string;
    label: string;
    /** Hover of another chart (bucket time): drawn here too. Null when charts are not synchronized. */
    syncTs?: number | null;
    /** Reports the bucket under the pointer (null: none), so the owner can sync the other charts. */
    onhover?: (ts: number | null) => void;
  } = $props();

  const H = $derived($isPhone ? 150 : 180);
  const PAD_T = 8;
  const PAD_B = 34; // x labels and event markers
  const Y1 = $derived(H - PAD_B);

  let width = $state(600);
  let hover = $state<number | null>(null); // index into `times`

  const X0 = 58;
  const X1 = $derived(Math.max(X0 + 40, width - 8));
  const stepSec = $derived(step > 0 ? step : 60);
  const span = $derived(Math.max(1, to - from));
  const single = $derived(series.length === 1);
  const hasData = $derived(series.some((s) => s.points.length > 0));

  /** Round axis step: 1, 2 or 5 times a power of ten (of 1024-units for bytes). */
  function niceStep(raw: number): number {
    if (raw <= 0) return 1;
    let unit = 1;
    if (binary) while (raw / unit >= 1024) unit *= 1024;
    const r = raw / unit;
    const mag = 10 ** Math.floor(Math.log10(r));
    const f = r / mag;
    const nice = f <= 1 ? 1 : f <= 2 ? 2 : f <= 5 ? 5 : 10;
    return nice * mag * unit;
  }

  const scale = $derived.by(() => {
    if (typeof yMax === 'number') {
      const st = yMax / 4;
      return { top: yMax, ticks: [0, 1, 2, 3, 4].map((i) => i * st) };
    }
    let max = 0;
    for (const s of series) for (const p of s.points) max = Math.max(max, single ? p.peak : p.value);
    max = Math.max(max, minTop);
    const st = niceStep(max / 4);
    const top = Math.ceil(max / st) * st;
    const ticks: number[] = [];
    for (let v = 0; v <= top + st / 2; v += st) ticks.push(v);
    return { top, ticks };
  });

  function x(ts: number): number {
    return X0 + ((ts - from) / span) * (X1 - X0);
  }

  function y(v: number): number {
    return Y1 - (Math.min(Math.max(v, 0), scale.top) / scale.top) * (Y1 - PAD_T);
  }

  /** Bucket centre on the time axis. */
  const mid = (p: ChartPoint) => p.ts + stepSec / 2;

  /** Splits a series where buckets are missing (agent offline). */
  function segments(points: ChartPoint[]): ChartPoint[][] {
    const out: ChartPoint[][] = [];
    let cur: ChartPoint[] = [];
    for (const p of points) {
      if (cur.length > 0 && p.ts - cur[cur.length - 1].ts > stepSec * 1.5) {
        out.push(cur);
        cur = [];
      }
      cur.push(p);
    }
    if (cur.length > 0) out.push(cur);
    return out;
  }

  function linePath(seg: ChartPoint[], key: 'value' | 'peak'): string {
    return seg.map((p, i) => `${i ? 'L' : 'M'}${x(mid(p)).toFixed(1)} ${y(p[key]).toFixed(1)}`).join('');
  }

  function areaPath(seg: ChartPoint[]): string {
    const first = x(mid(seg[0])).toFixed(1);
    const last = x(mid(seg[seg.length - 1])).toFixed(1);
    return `${linePath(seg, 'value')}L${last} ${Y1}L${first} ${Y1}Z`;
  }

  const drawn = $derived(
    series.map((s) => ({ ...s, segs: segments(s.points) })),
  );

  /** All bucket times present in any series, sorted; hover snaps to them. */
  const times = $derived.by(() => {
    const set = new Set<number>();
    for (const s of series) for (const p of s.points) set.add(p.ts);
    return [...set].sort((a, b) => a - b);
  });

  // Five labels when they fit side by side (11 px glyphs are about 6.7 px wide; the middle ones are centered under their
  // tick), three when the labels are long or the chart is narrow (a phone, "06:45 PM").
  const xTicks = $derived(
    (((X1 - X0) / 4 >= tickLabel(from).length * 6.7 * 1.5 + 4) ? [0, 0.25, 0.5, 0.75, 1] : [0, 0.5, 1]).map((f) => from + f * span),
  );

  const marks = $derived(
    events
      .filter((e) => e.ts >= from && e.ts <= to)
      .map((e) => ({ x: x(e.ts), kind: e.kind, ts: e.ts })),
  );

  // The own pointer wins; otherwise the synchronized one, if this chart has that bucket.
  const hoverTs = $derived.by(() => {
    if (hover !== null) return times[hover];
    return syncTs !== null && times.includes(syncTs) ? syncTs : null;
  });
  const hoverRows = $derived(
    hoverTs === null ? [] : series.map((s) => ({ s, p: s.points.find((p) => p.ts === hoverTs) })),
  );
  const hoverEvents = $derived(
    hoverTs === null ? [] : events.filter((e) => e.ts >= hoverTs && e.ts < hoverTs + stepSec),
  );

  function onMove(event: PointerEvent): void {
    const svg = event.currentTarget as SVGSVGElement;
    const rect = svg.getBoundingClientRect();
    const px = ((event.clientX - rect.left) / rect.width) * width;
    if (times.length === 0 || px < X0 - 4 || px > X1 + 4) {
      hover = null;
      onhover?.(null);
      return;
    }
    // Nearest bucket by x, but only within a few buckets: no tooltip over an empty stretch.
    let best = -1;
    let dist = Infinity;
    for (let i = 0; i < times.length; i++) {
      const d = Math.abs(x(times[i] + stepSec / 2) - px);
      if (d < dist) {
        dist = d;
        best = i;
      }
    }
    const slotPx = ((X1 - X0) * stepSec) / span;
    hover = dist <= Math.max(12, slotPx * 1.5) ? best : null;
    onhover?.(hover === null ? null : times[hover]);
  }

  function onLeave(event?: PointerEvent): void {
    // A finger lifted: the values stay until the next touch (the tip is the only way to read them on a phone).
    if (event?.pointerType === 'touch') return;
    hover = null;
    onhover?.(null);
  }
</script>

<div class="chart" bind:clientWidth={width}>
  {#if !hasData}
    <p class="empty muted">{$tr('srv.chart.noData')}</p>
  {:else}
    <p class="legend small">
      {#if single}
        <span><i class="sw line-sw" style:background={series[0].color}></i>{$tr('srv.chart.avgLegend')}</span>
        <span><i class="sw dash-sw" style:border-color={series[0].color}></i>{$tr('srv.chart.peakLegend')}</span>
      {:else}
        {#each series as s, i (i)}
          <span><i class="sw line-sw" style:background={s.color}></i>{s.label}</span>
        {/each}
      {/if}
    </p>
    <svg
      viewBox="0 0 {width} {H}"
      height={H}
      role="img"
      aria-label={label}
      onpointermove={onMove}
      onpointerdown={(e) => e.pointerType === 'touch' && onMove(e)}
      onpointerleave={onLeave}
    >
      {#each scale.ticks as tv (tv)}
        <line class="grid" class:zero={tv === 0} x1={X0} x2={X1} y1={y(tv)} y2={y(tv)} />
        <text class="ax" x={X0 - 8} y={y(tv) + 4} text-anchor="end">{format(tv)}</text>
      {/each}

      {#each marks as m, i (i)}
        {#if m.kind === 'reboot'}
          <line class="reboot" x1={m.x} x2={m.x} y1={PAD_T} y2={Y1} />
        {/if}
      {/each}

      {#each drawn as s, si (si)}
        {#each s.segs as seg, gi (gi)}
          {#if seg.length > 1}
            <path d={areaPath(seg)} fill={s.color} class="area" class:faint={!single} />
            {#if single}
              <path d={linePath(seg, 'peak')} stroke={s.color} class="peak" />
            {/if}
            <path d={linePath(seg, 'value')} stroke={s.color} class="line" />
          {:else}
            <circle cx={x(mid(seg[0]))} cy={y(seg[0].value)} r="3" fill={s.color} />
          {/if}
        {/each}
      {/each}

      {#if hoverTs !== null}
        <line class="cross" x1={x(hoverTs + stepSec / 2)} x2={x(hoverTs + stepSec / 2)} y1={PAD_T} y2={Y1} />
        {#each hoverRows as r, i (i)}
          {#if r.p}
            <circle class="dot" cx={x(mid(r.p))} cy={y(r.p.value)} r="4" fill={r.s.color} />
          {/if}
        {/each}
      {/if}

      {#each marks as m, i (i)}
        <polygon class="tri" class:crit={m.kind === 'reboot'} points="{m.x},{Y1 + 3} {m.x - 4},{Y1 + 10} {m.x + 4},{Y1 + 10}">
          <title>{$tr(`srv.event.${m.kind}`)} · {fullLabel(m.ts)}</title>
        </polygon>
      {/each}

      {#each xTicks as ts, i (i)}
        <text class="ax" x={x(ts)} y={H - 6} text-anchor={i === 0 ? 'start' : i === xTicks.length - 1 ? 'end' : 'middle'}
          >{tickLabel(ts)}</text
        >
      {/each}
    </svg>

    {#if hoverTs !== null}
      {@const left = (x(hoverTs + stepSec / 2) / width) * 100}
      <div class="tip" class:flip={left > 55} style:left="{left}%">
        <strong>{fullLabel(hoverTs)}</strong>
        {#each hoverRows as r, i (i)}
          <span>
            <i class="key" style:background={r.s.color}></i>{r.s.label}
            {#if r.p}
              {$tr('srv.chart.avg')} <b>{format(r.p.value)}</b> · {$tr('srv.chart.peak')} <b>{format(r.p.peak)}</b>
            {:else}
              —
            {/if}
          </span>
        {/each}
        {#each hoverEvents as e, i (i)}
          <span>▲ {$tr(`srv.event.${e.kind}`)} · {fullLabel(e.ts)}</span>
        {/each}
      </div>
    {/if}
  {/if}
</div>

<style>
  .chart {
    position: relative;
    min-width: 0;
  }

  svg {
    display: block;
    width: 100%;
    overflow: visible;
    user-select: none;
    touch-action: pan-y;
  }

  .grid {
    stroke: var(--seg-off);
    stroke-width: 1;
  }

  .grid.zero {
    stroke: var(--line);
    stroke-width: 1.5;
  }

  .ax {
    fill: var(--muted);
    font-size: 10px;
    font-family: var(--font-body);
  }

  .area {
    opacity: 0.22;
    stroke: none;
  }

  .area.faint {
    opacity: 0.1;
  }

  .line {
    fill: none;
    stroke-width: 2;
    stroke-linejoin: round;
    stroke-linecap: round;
  }

  .peak {
    fill: none;
    stroke-width: 1;
    stroke-dasharray: 3 3;
    opacity: 0.8;
  }

  .reboot {
    stroke: var(--crit);
    stroke-width: 1;
    stroke-dasharray: 2 3;
  }

  .cross {
    stroke: var(--muted);
    stroke-width: 1;
  }

  .dot {
    stroke: var(--surface);
    stroke-width: 2;
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
    transform: translateX(12px);
    display: flex;
    flex-direction: column;
    gap: 3px;
    padding: 5px 7px;
    background: var(--ink);
    color: var(--surface);
    border: 2px solid var(--line);
    font-size: 11px;
    white-space: nowrap;
    pointer-events: none;
    z-index: 1;
  }

  .tip.flip {
    transform: translateX(calc(-100% - 12px));
  }

  .tip b {
    font-weight: 400;
  }

  .key {
    display: inline-block;
    width: 7px;
    height: 7px;
    margin-right: 5px;
  }

  .legend {
    display: flex;
    flex-wrap: wrap;
    gap: 4px 13px;
    margin: 0 0 4px;
    color: var(--muted);
  }

  .sw {
    display: inline-block;
    width: 13px;
    margin-right: 5px;
    vertical-align: middle;
  }

  .line-sw {
    height: 3px;
  }

  .dash-sw {
    height: 0;
    border-top: 1px dashed;
  }

  .empty {
    margin: 0;
    padding: 22px 0;
    text-align: center;
  }
</style>
