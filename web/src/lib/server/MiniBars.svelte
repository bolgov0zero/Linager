<script lang="ts">
  // Last-minute mini bars for the overview panels. Values scale against `max`;
  // null (no data yet) draws nothing. Newest bar is dark.
  const SLOTS = 60;

  let { values, max = 100, color = 'var(--accent)' }: { values: (number | null)[]; max?: number; color?: string } =
    $props();

  const padded = $derived.by(() => {
    const pad: (number | null)[] = Array.from({ length: Math.max(0, SLOTS - values.length) }, () => null);
    return [...pad, ...values.slice(-SLOTS)];
  });
</script>

<span class="mini" style:--bar={color} aria-hidden="true">
  {#each padded as v, i (i)}
    <i
      class:last={i === padded.length - 1}
      style:height="{v === null ? 0 : Math.max(3, Math.min(1, v / max) * 100)}%"
    ></i>
  {/each}
</span>

<style>
  .mini {
    display: flex;
    align-items: flex-end;
    gap: 1px;
    width: 100%;
    height: 40px;
  }

  i {
    flex: 1;
    min-width: 1px;
    background: var(--bar);
  }

  i.last {
    background: var(--ink);
  }
</style>
