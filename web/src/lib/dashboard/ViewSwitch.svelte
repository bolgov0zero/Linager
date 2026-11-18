<script lang="ts">
  import { tr } from '../../i18n';
  import PixIcon from '../ui/PixIcon.svelte';
  import { isPhone } from '../ui/viewport';
  import { dashView, setDashView } from './view';
</script>

{#if $isPhone}
  <!-- Phone: one icon that shows the view a tap switches to. -->
  <button
    class="btn ic"
    type="button"
    aria-label={$tr('dash.viewToggle')}
    onclick={() => setDashView($dashView === 'cards' ? 'map' : 'cards')}>
    <PixIcon name={$dashView === 'cards' ? 'map' : 'cards'} />
  </button>
{:else}
  <div class="seg" role="group" aria-label={$tr('dash.viewToggle')}>
    <button class="chip" type="button" aria-pressed={$dashView === 'cards'} onclick={() => setDashView('cards')}>{$tr('dash.view.cards')}</button>
    <button class="chip" type="button" aria-pressed={$dashView === 'map'} onclick={() => setDashView('map')}>{$tr('dash.view.map')}</button>
  </div>
{/if}

<style>
  .seg {
    display: flex;
    gap: 5px;
  }

  .ic {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 44px;
    min-height: 44px;
    padding: 0;
  }
</style>
