<script lang="ts">
  import PixIcon from '../ui/PixIcon.svelte';
  import { tr } from '../../i18n';

  let {
    index,
    count,
    onmove,
    onremove,
  }: {
    index: number;
    count: number;
    onmove: (dir: -1 | 1) => void;
    onremove: () => void;
  } = $props();
</script>

<div class="edit row">
  <button
    class="btn sm"
    type="button"
    disabled={index <= 0}
    title={$tr('dash.move.prev')}
    aria-label={$tr('dash.move.prev')}
    onclick={() => onmove(-1)}><PixIcon name="chevron-left" size={14} /></button>
  <button
    class="btn sm"
    type="button"
    disabled={index >= count - 1}
    title={$tr('dash.move.next')}
    aria-label={$tr('dash.move.next')}
    onclick={() => onmove(1)}><PixIcon name="next" size={14} /></button>
  <button
    class="btn sm"
    type="button"
    title={$tr('dash.remove')}
    aria-label={$tr('dash.remove')}
    onclick={onremove}><PixIcon name="x" size={14} /></button>
</div>

<style>
  .edit {
    flex-wrap: nowrap;
    margin-left: auto;
    gap: 5px;
  }

  /* Square icon buttons, as in the design's edit mode. */
  .sm {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 25px;
    height: 25px;
    padding: 0;
    font-size: 14px;
    line-height: 1;
    box-shadow: 2px 2px 0 var(--shade);
  }

  @media (max-width: 720px) {
    .sm {
      width: 44px;
      height: 44px;
    }
  }
</style>
