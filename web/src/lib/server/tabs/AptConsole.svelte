<script lang="ts">
  import { tr } from '../../../i18n';

  let { text }: { text: string } = $props();

  // Hidden by default: the statuses above say everything most of the time.
  let open = $state(false);
  let logEl = $state<HTMLPreElement | undefined>(undefined);

  // Keep the console scrolled to the newest output.
  $effect(() => {
    void text;
    if (open && logEl) logEl.scrollTop = logEl.scrollHeight;
  });
</script>

<div class="console">
  <button class="btn small-btn" type="button" aria-expanded={open} onclick={() => (open = !open)}>
    {open ? $tr('apt.console.hide') : $tr('apt.console.show')}
  </button>
  {#if open}
    <pre class="log" bind:this={logEl} aria-label={$tr('apt.log')}>{text}</pre>
  {/if}
</div>

<style>
  .console {
    display: flex;
    flex-direction: column;
    gap: 8px;
    min-width: 0;
  }

  .small-btn {
    align-self: flex-start;
    padding: 2px 8px;
    font-size: 13px;
  }

  .log {
    margin: 0;
    padding: 10px;
    max-height: 320px;
    overflow: auto;
    border: 2px solid var(--line);
    background: var(--ink);
    color: var(--surface);
    font-family: var(--font-body);
    font-size: 12px;
    line-height: 1.5;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
</style>
