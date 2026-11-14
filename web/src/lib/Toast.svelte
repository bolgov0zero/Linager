<script lang="ts">
  import { toast } from './toast';
</script>

{#if $toast}
  <div class="toast" role="status" aria-live="polite">{$toast}</div>
{/if}
