<script lang="ts">
  /** Pixel icons for the terminal buttons on phones, drawn on a 16x16 grid like the logo. */
  let { name }: { name: 'copy' | 'paste' | 'window' | 'close' | 'bolt' } = $props();
</script>

<svg viewBox="0 0 16 16" width="16" height="16" shape-rendering="crispEdges" fill="currentColor" aria-hidden="true">
  {#if name === 'copy'}
    <rect x="6" y="1" width="8" height="2" />
    <rect x="12" y="1" width="2" height="9" />
    <rect x="6" y="1" width="2" height="4" />
    <rect x="2" y="5" width="8" height="2" />
    <rect x="2" y="13" width="8" height="2" />
    <rect x="2" y="5" width="2" height="10" />
    <rect x="8" y="5" width="2" height="10" />
  {:else if name === 'paste'}
    <rect x="5" y="1" width="6" height="3" />
    <rect x="2" y="3" width="3" height="2" />
    <rect x="11" y="3" width="3" height="2" />
    <rect x="2" y="3" width="2" height="11" />
    <rect x="12" y="3" width="2" height="11" />
    <rect x="2" y="12" width="12" height="2" />
    <rect x="5" y="7" width="6" height="1" />
    <rect x="5" y="9" width="6" height="1" />
  {:else if name === 'window'}
    <rect x="1" y="2" width="14" height="4" />
    <rect x="1" y="2" width="2" height="12" />
    <rect x="13" y="2" width="2" height="12" />
    <rect x="1" y="12" width="14" height="2" />
  {:else if name === 'bolt'}
    <rect x="9" y="1" width="5" height="1" />
    <rect x="8" y="2" width="5" height="1" />
    <rect x="7" y="3" width="5" height="1" />
    <rect x="6" y="4" width="5" height="1" />
    <rect x="5" y="5" width="5" height="1" />
    <rect x="4" y="6" width="9" height="1" />
    <rect x="4" y="7" width="9" height="1" />
    <rect x="8" y="8" width="5" height="1" />
    <rect x="7" y="9" width="5" height="1" />
    <rect x="6" y="10" width="5" height="1" />
    <rect x="5" y="11" width="5" height="1" />
    <rect x="4" y="12" width="4" height="1" />
    <rect x="3" y="13" width="3" height="1" />
    <rect x="3" y="14" width="2" height="1" />
  {:else}
    <rect x="3" y="3" width="2" height="2" />
    <rect x="5" y="5" width="2" height="2" />
    <rect x="7" y="7" width="2" height="2" />
    <rect x="9" y="9" width="2" height="2" />
    <rect x="11" y="11" width="2" height="2" />
    <rect x="11" y="3" width="2" height="2" />
    <rect x="9" y="5" width="2" height="2" />
    <rect x="5" y="9" width="2" height="2" />
    <rect x="3" y="11" width="2" height="2" />
  {/if}
</svg>
