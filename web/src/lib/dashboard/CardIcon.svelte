<script lang="ts">
  /** Small pixel icons for the card buttons, drawn on a 16x16 grid like the logo. */
  let { name }: { name: 'terminal' | 'open' | 'trash' | 'assistant' } = $props();

  /** The Assistant: a monitor with a mouse pointer, on an 8x8 grid of 2 px squares. */
  const ASSISTANT = ['11111111', '10000001', '10100001', '10110001', '10111001', '10100001', '11111111', '00111100'];
</script>

<svg viewBox="0 0 16 16" width="16" height="16" shape-rendering="crispEdges" fill="currentColor" aria-hidden="true">
  {#if name === 'terminal'}
    <rect x="2" y="3" width="2" height="2" />
    <rect x="4" y="5" width="2" height="2" />
    <rect x="6" y="7" width="2" height="2" />
    <rect x="4" y="9" width="2" height="2" />
    <rect x="2" y="11" width="2" height="2" />
    <rect x="9" y="12" width="5" height="2" />
  {:else if name === 'assistant'}
    {#each ASSISTANT as row, y (y)}
      {#each [...row] as cell, x (x)}
        {#if cell === '1'}<rect x={x * 2} y={y * 2} width="2" height="2" />{/if}
      {/each}
    {/each}
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
