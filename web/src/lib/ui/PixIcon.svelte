<script lang="ts">
  import { iconRows } from './pixicons';

  /** A 9x9 pixel icon drawn at 18 px: every square is exactly 2 px, so it stays sharp on 1x, 2x and 3x screens. */
  let { name }: { name: string } = $props();
</script>

<svg class="pix" viewBox="0 0 9 9" width="18" height="18" fill="currentColor" shape-rendering="crispEdges" aria-hidden="true">
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
