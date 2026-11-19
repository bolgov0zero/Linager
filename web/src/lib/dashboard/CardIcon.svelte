<script lang="ts">
  import PixIcon from '../ui/PixIcon.svelte';
  /** The icons of the card buttons. */
  let { name }: { name: 'terminal' | 'open' | 'trash' | 'assistant' } = $props();
</script>

<PixIcon {name} size={16} />
