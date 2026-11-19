<script lang="ts">
  import PixIcon from '../ui/PixIcon.svelte';
  /** The icons of the terminal buttons on phones. */
  let { name }: { name: 'copy' | 'paste' | 'window' | 'close' | 'bolt' } = $props();
</script>

<PixIcon name={name === 'close' ? 'x' : name} size={16} />
