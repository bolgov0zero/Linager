<script lang="ts">
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
    onclick={() => onmove(-1)}>‹</button>
  <button
    class="btn sm"
    type="button"
    disabled={index >= count - 1}
    title={$tr('dash.move.next')}
    aria-label={$tr('dash.move.next')}
    onclick={() => onmove(1)}>›</button>
  <button
    class="btn sm"
    type="button"
    title={$tr('dash.remove')}
    aria-label={$tr('dash.remove')}
    onclick={onremove}>×</button>
</div>

<style>
  .edit {
    flex-wrap: nowrap;
    margin-left: auto;
    gap: 6px;
  }

  /* Square icon buttons, as in the design's edit mode. */
  .sm {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 28px;
    height: 28px;
    padding: 0;
    font-size: 16px;
    line-height: 1;
    box-shadow: 2px 2px 0 var(--shade);
  }
</style>
