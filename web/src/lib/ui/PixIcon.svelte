<script lang="ts">
  import { iconPath } from './pixicons';

  /**
   * An icon of Pixelarticons (24x24 grid, one square is 1/24 of the size). It is sharpest where a square is a whole number of
   * device pixels: 24 px (and 12 px on a 2x screen); other sizes are scaled and a little softer.
   */
  let { name, size = 18 }: { name: string; size?: number } = $props();
</script>

<svg class="pix" viewBox="0 0 24 24" width={size} height={size} fill="currentColor" aria-hidden="true"><path d={iconPath(name)} /></svg>

<style>
  .pix {
    flex: none;
    display: block;
  }
</style>
