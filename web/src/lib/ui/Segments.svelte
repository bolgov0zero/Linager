<script lang="ts">
  // 20-segment gauge: above 75% yellow, above 90% red; null = offline (greyed).
  // plain: a progress bar, always the accent colour. busy: unknown progress, a light runs along the bar.
  let {
    value,
    segments = 20,
    plain = false,
    busy = false,
  }: { value: number | null; segments?: number; plain?: boolean; busy?: boolean } = $props();
  const filled = $derived(value === null ? 0 : Math.round((Math.min(100, Math.max(0, value)) / 100) * segments));
  const tone = $derived(
    value === null ? 'off' : plain ? 'ok' : value > 90 ? 'crit' : value > 75 ? 'warn' : 'ok',
  );
</script>

<span class="seg {tone}" class:busy aria-hidden="true">
  {#each Array(segments) as _, i}<i class:on={!busy && i < filled} style={busy ? `--i:${i}` : undefined}></i>{/each}
</span>

<style>
  .seg { display: inline-flex; gap: 2px; width: 100%; }
  i { flex: 1; height: 9px; background: var(--seg-off); }
  .ok i.on { background: var(--accent); }
  .warn i.on { background: var(--warn); }
  .crit i.on { background: var(--crit); }
  .busy i { animation: seg-run 1.6s steps(1) infinite; animation-delay: calc(var(--i) * 80ms); }
  @keyframes seg-run {
    0%, 25% { background: var(--accent); }
    26%, 100% { background: var(--seg-off); }
  }
  @media (prefers-reduced-motion: reduce) {
    .busy i { animation: none; }
    .busy i:nth-child(-n + 4) { background: var(--accent); }
  }
</style>
