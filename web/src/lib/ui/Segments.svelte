<script lang="ts">
  // 20-segment gauge: above 75% yellow, above 90% red; null = offline (greyed).
  let { value, segments = 20 }: { value: number | null; segments?: number } = $props();
  const filled = $derived(value === null ? 0 : Math.round((Math.min(100, Math.max(0, value)) / 100) * segments));
  const tone = $derived(value === null ? 'off' : value > 90 ? 'crit' : value > 75 ? 'warn' : 'ok');
</script>

<span class="seg {tone}" aria-hidden="true">
  {#each Array(segments) as _, i}<i class:on={i < filled}></i>{/each}
</span>

<style>
  .seg { display: inline-flex; gap: 2px; width: 100%; }
  i { flex: 1; height: 10px; background: var(--seg-off); }
  .ok i.on { background: var(--accent); }
  .warn i.on { background: var(--warn); }
  .crit i.on { background: var(--crit); }
</style>
