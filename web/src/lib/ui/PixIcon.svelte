<script lang="ts">
  import { iconRows } from './pixicons';

  let { name, size = 20 }: { name: string; size?: number } = $props();
</script>

<svg class="pix" viewBox="0 0 8 8" width={size} height={size} fill="currentColor" shape-rendering="crispEdges" aria-hidden="true">
  {#each iconRows(name) as row, y (y)}
    {#each [...row] as cell, x (x)}
      {#if cell === '1'}<rect {x} {y} width="1" height="1" />{/if}
    {/each}
  {/each}
</svg>

<style>
  .pix {
    flex: none;
    display: block;
  }
</style>
