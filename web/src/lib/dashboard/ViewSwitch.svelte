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
    onclick={() => setDashView($dashView === 'map' ? 'cards' : 'map')}>
    <PixIcon name={$dashView === 'map' ? 'cards' : 'map'} />
  </button>
{:else}
  <div class="seg" role="group" aria-label={$tr('dash.viewToggle')}>
    <button class="chip" type="button" aria-pressed={$dashView === 'cards'} onclick={() => setDashView('cards')}>{$tr('dash.view.cards')}</button>
    <button class="chip" type="button" aria-pressed={$dashView === 'map'} onclick={() => setDashView('map')}>{$tr('dash.view.map')}</button>
    <button class="chip" type="button" aria-pressed={$dashView === 'pult'} onclick={() => setDashView('pult')}>{$tr('dash.view.pult')}</button>
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
