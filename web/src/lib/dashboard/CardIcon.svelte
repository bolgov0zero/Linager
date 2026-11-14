<script lang="ts">
  /** Small pixel icons for the card buttons, drawn on a 16x16 grid like the logo. */
  let { name }: { name: 'terminal' | 'open' | 'trash' } = $props();
</script>

<svg viewBox="0 0 16 16" width="18" height="18" shape-rendering="crispEdges" fill="currentColor" aria-hidden="true">
  {#if name === 'terminal'}
    <rect x="2" y="3" width="2" height="2" />
    <rect x="4" y="5" width="2" height="2" />
    <rect x="6" y="7" width="2" height="2" />
    <rect x="4" y="9" width="2" height="2" />
    <rect x="2" y="11" width="2" height="2" />
    <rect x="9" y="12" width="5" height="2" />
  {:else if name === 'open'}
    <rect x="2" y="2" width="12" height="2" />
    <rect x="2" y="12" width="12" height="2" />
    <rect x="2" y="2" width="2" height="12" />
    <rect x="12" y="2" width="2" height="12" />
    <rect x="5" y="7" width="6" height="2" />
    <rect x="8" y="5" width="2" height="2" />
    <rect x="8" y="9" width="2" height="2" />
  {:else}
    <rect x="6" y="1" width="4" height="2" />
    <rect x="2" y="3" width="12" height="2" />
    <rect x="4" y="6" width="2" height="8" />
    <rect x="10" y="6" width="2" height="8" />
    <rect x="4" y="12" width="8" height="2" />
    <rect x="7" y="7" width="2" height="5" />
  {/if}
</svg>
