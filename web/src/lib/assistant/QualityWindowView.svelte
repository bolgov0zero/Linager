<script lang="ts">
  import { tr } from '../../i18n';
  import WindowFrame from '../terminal/WindowFrame.svelte';
  import type { TermWindow } from '../terminal/windows';
  import AssistantQuality from './AssistantQuality.svelte';

  /** The picture settings of the Assistant in a floating window: change them beside the screen and see the effect on the line under it. */
  let { win, top }: { win: TermWindow; top: boolean } = $props();
</script>

<WindowFrame {win} {top} ariaLabel={$tr('qualitywin.title', { name: win.name })}>
  {#snippet title()}
    <span class="label">{win.name}</span>
    <span class="muted status">· {$tr('assist.qualityShort')}</span>
  {/snippet}
  <div class="fill"><AssistantQuality serverId={win.serverId} /></div>
</WindowFrame>

<style>
  .fill {
    height: 100%;
    overflow: auto;
    padding: 4px 14px 14px;
    background: var(--surface);
  }
</style>
