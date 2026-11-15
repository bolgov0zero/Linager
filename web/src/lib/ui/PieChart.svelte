<script lang="ts">
  import { formatBytes } from '../server/format';

  export interface PiePart {
    id: string;
    name: string;
    value: number;
    color: string;
  }

  let {
    parts,
    size = 150,
    legend = true,
    showShare = false,
  }: {
    parts: PiePart[];
    size?: number;
    /** A table next to the chart; hovering a row lifts its sector. */
    legend?: boolean;
    /** A column with the share of each part. */
    showShare?: boolean;
  } = $props();

  const LIFT = 9;

  let hovered = $state<string | null>(null);
  /** The tooltip follows only a pointer that is over the chart, not one over the legend. */
  let tip = $state<{ x: number; y: number } | null>(null);
  let host = $state<HTMLDivElement | undefined>(undefined);

  const total = $derived(parts.reduce((n, p) => n + p.value, 0));
  const r = $derived(size / 2);

  /** Path of every sector, with the angle of its middle (the direction it is lifted in). */
  const sectors = $derived.by(() => {
    const out: { part: PiePart; d: string; mid: number }[] = [];
    if (total <= 0) return out;
    let a0 = -Math.PI / 2;
    const list = parts.filter((p) => p.value > 0);
    for (const part of list) {
      const a1 = a0 + (part.value / total) * 2 * Math.PI;
      const big = a1 - a0 > Math.PI ? 1 : 0;
      const x0 = r + r * Math.cos(a0);
      const y0 = r + r * Math.sin(a0);
      const x1 = r + r * Math.cos(a1);
      const y1 = r + r * Math.sin(a1);
      const d =
        list.length === 1
          ? `M${r} 0 A${r} ${r} 0 1 1 ${r - 0.01} 0 Z`
          : `M${r} ${r} L${x0.toFixed(2)} ${y0.toFixed(2)} A${r} ${r} 0 ${big} 1 ${x1.toFixed(2)} ${y1.toFixed(2)} Z`;
      out.push({ part, d, mid: (a0 + a1) / 2 });
      a0 = a1;
    }
    return out;
  });

  const lifted = $derived(sectors.find((s) => s.part.id === hovered) ?? null);

  function share(v: number): string {
    return total > 0 ? `${Math.round((v / total) * 100)}%` : '—';
  }

  function enter(id: string, e: PointerEvent | null): void {
    hovered = id;
    tip = e ? place(e) : null;
  }

  function place(e: PointerEvent): { x: number; y: number } {
    const box = host?.getBoundingClientRect();
    return { x: e.clientX - (box?.left ?? 0) + 14, y: e.clientY - (box?.top ?? 0) - 34 };
  }

  function leave(): void {
    hovered = null;
    tip = null;
  }
</script>

<div class="pie-block">
  <div
    class="piewrap"
    bind:this={host}
    role="img"
    aria-label={parts.map((p) => `${p.name}: ${formatBytes(p.value)}`).join(', ')}
    onpointerleave={leave}
    onpointercancel={leave}
  >
    <svg class="pie" width={size} height={size} viewBox="0 0 {size} {size}" aria-hidden="true">
      {#each sectors as s (s.part.id)}
        <!-- the sector under the pointer is never moved: a copy is lifted above it, so the events stay stable -->
        <path
          role="presentation"
          d={s.d}
          fill={s.part.color}
          style:opacity={hovered === s.part.id ? 0 : undefined}
          onpointerenter={(e) => enter(s.part.id, e)}
          onpointermove={(e) => {
            if (hovered === s.part.id) tip = place(e);
          }}
          onpointerleave={leave}
        />
      {/each}
      {#if lifted}
        <g pointer-events="none">
          <path
            d={lifted.d}
            fill={lifted.part.color}
            style:transform="translate({(Math.cos(lifted.mid) * LIFT).toFixed(1)}px, {(Math.sin(lifted.mid) * LIFT).toFixed(1)}px)"
          />
        </g>
      {/if}
    </svg>
    {#if lifted && tip}
      <div class="pietip" style:left="{tip.x}px" style:top="{tip.y}px">
        <b>{lifted.part.name}</b> · {formatBytes(lifted.part.value)} · {share(lifted.part.value)}
      </div>
    {/if}
  </div>

  {#if legend}
    <table class="legend">
      <tbody>
        {#each parts as p (p.id)}
          <tr class:on={hovered === p.id} onpointerenter={() => enter(p.id, null)} onpointerleave={leave}>
            <td><i class="sw" style:background={p.color}></i>{p.name}</td>
            <td class="num mono">{formatBytes(p.value)}</td>
            {#if showShare}<td class="num muted">{share(p.value)}</td>{/if}
          </tr>
        {/each}
      </tbody>
    </table>
  {/if}
</div>

<style>
  .pie-block {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 14px 24px;
    min-width: 0;
  }

  .piewrap {
    position: relative;
    flex: none;
    padding: 10px;
  }

  .pie {
    display: block;
    overflow: visible;
  }

  .pie path {
    stroke: var(--surface);
    stroke-width: 2;
    stroke-linejoin: miter;
    cursor: default;
  }

  .pietip {
    position: absolute;
    z-index: 6;
    pointer-events: none;
    padding: 3px 8px;
    border: 2px solid var(--line);
    background: var(--ink);
    color: var(--surface);
    font-size: 12px;
    white-space: nowrap;
    box-shadow: 3px 3px 0 var(--shade);
  }

  .legend {
    flex: 1 1 220px;
    min-width: 0;
    border-collapse: collapse;
    font-size: 13px;
  }

  .legend td {
    padding: 7px 8px;
    border-bottom: 2px dashed var(--seg-off);
    cursor: default;
  }

  .legend tr.on td {
    background: color-mix(in srgb, var(--accent) 14%, transparent);
    font-weight: 600;
  }

  .sw {
    display: inline-block;
    width: 12px;
    height: 12px;
    margin-right: 8px;
    vertical-align: -1px;
  }

  .num {
    text-align: right;
    white-space: nowrap;
  }
</style>
