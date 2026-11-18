<script lang="ts">
  import { tr } from '../../i18n';
  import type { Tag } from './status';

  let { tags }: { tags: Tag[] } = $props();
</script>

<div class="tags">
  {#each tags as tag (tag.id)}
    <span class="tag {tag.tone}">
      {#if tag.id === 'reboot'}
        {$tr('dash.tag.reboot')}
      {:else if tag.id === 'updates'}
        {$tr('dash.tag.updates', { n: tag.n ?? 0 })}
      {:else if tag.id === 'images'}
        {$tr('dash.tag.images', { n: tag.n ?? 0 })}
      {:else if tag.id === 'failed'}
        {$tr('dash.tag.failed', { n: tag.n ?? 0 })}
      {:else if tag.id === 'agentUpdate'}
        {$tr('dash.tag.agentUpdate')}
      {:else if tag.id === 'agentOld'}
        {$tr('dash.tag.agentOld')}
      {:else if tag.id === 'offline'}
        {$tr('dash.tag.offline')}
      {:else}
        {$tr('dash.tag.clean')}
      {/if}
    </span>
  {/each}
</div>

<style>
  .tags {
    display: flex;
    flex-wrap: wrap;
    gap: 5px;
  }

  .tag {
    padding: 2px 5px;
    border: 2px solid var(--line);
    background: var(--surface);
    font-size: 11px;
    line-height: 1.3;
  }

  .tag.warn {
    border-color: var(--warn);
    color: var(--warn);
  }

  .tag.crit {
    border-color: var(--crit);
    color: var(--crit);
  }

  .tag.ok {
    border-color: var(--good);
    color: var(--good);
  }

  .tag.muted {
    border-color: var(--muted);
    color: var(--muted);
  }
</style>
