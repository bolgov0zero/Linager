<script lang="ts">
  // Mini bar chart of the last values (0-100); the newest bar is dark.
  let { values, count = 12, height = 28 }: { values: number[]; count?: number; height?: number } = $props();
  const padded = $derived([...Array(Math.max(0, count - values.length)).fill(null), ...values.slice(-count)] as (number | null)[]);
</script>

<span class="spark" style:height="{height}px" aria-hidden="true">
  {#each padded as v, i}
    <i class:last={i === padded.length - 1} style:height="{v === null ? 0 : Math.max(2, (v / 100) * height)}px"></i>
  {/each}
</span>

<style>
  .spark { display: flex; align-items: flex-end; gap: 2px; width: 100%; }
  i { flex: 1; background: var(--accent); }
  i.last { background: var(--ink); }
</style>
