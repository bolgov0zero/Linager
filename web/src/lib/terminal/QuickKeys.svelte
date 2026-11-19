<script lang="ts">
  import PixIcon from '../ui/PixIcon.svelte';
  import { tr } from '../../i18n';

  /**
   * The one row of special keys above the phone keyboard: Esc, Tab, Ctrl and Alt (latched), the arrows, "More" for
   * the extended panel and a button that hides the keyboard. A press must not take the focus from the terminal,
   * so the soft keyboard stays where it is.
   */
  let {
    ctrl,
    alt,
    more,
    onkey,
    ontoggle,
    onmore,
    onhide,
  }: {
    ctrl: boolean;
    alt: boolean;
    /** The extended panel is open. */
    more: boolean;
    onkey: (name: string) => void;
    ontoggle: (mod: 'ctrl' | 'alt') => void;
    onmore: () => void;
    onhide: () => void;
  } = $props();

  const keep = (e: PointerEvent) => e.preventDefault();

  const ARROW_ICON: Record<string, string> = { '←': 'arrow-left', '↑': 'arrow-up', '↓': 'arrow-down', '→': 'arrow-right' };
  const ARROWS: { key: string; label: string; name: string }[] = [
    { key: 'Left', label: '←', name: 'Left' },
    { key: 'Up', label: '↑', name: 'Up' },
    { key: 'Down', label: '↓', name: 'Down' },
    { key: 'Right', label: '→', name: 'Right' },
  ];
</script>

<div class="quick" role="group" aria-label={$tr('term.keys')}>
  <button class="k" type="button" onpointerdown={keep} onclick={() => onkey('Esc')}>Esc</button>
  <button class="k" type="button" onpointerdown={keep} onclick={() => onkey('Tab')}>Tab</button>
  <button class="k" class:on={ctrl} type="button" aria-pressed={ctrl} onpointerdown={keep} onclick={() => ontoggle('ctrl')}>Ctrl</button>
  <button class="k" class:on={alt} type="button" aria-pressed={alt} onpointerdown={keep} onclick={() => ontoggle('alt')}>Alt</button>
  {#each ARROWS as a (a.key)}
    <button class="k arrow" type="button" aria-label={a.name} onpointerdown={keep} onclick={() => onkey(a.key)}><PixIcon name={ARROW_ICON[a.label]} size={16} /></button>
  {/each}
  <button class="k" class:on={more} type="button" aria-pressed={more} onpointerdown={keep} onclick={onmore}>{$tr('m.term.more')}</button>
  <button class="k arrow" type="button" aria-label={$tr('m.term.hideKbd')} onpointerdown={keep} onclick={onhide}><PixIcon name="chev" size={16} /></button>
</div>

<style>
  .quick {
    display: grid;
    grid-template-columns: repeat(10, minmax(0, 1fr));
    gap: 4px;
    padding: 4px;
    background: var(--surface);
    border-top: 2px solid var(--line);
    user-select: none;
    -webkit-user-select: none;
    touch-action: manipulation;
  }

  .k {
    min-width: 0;
    min-height: 44px;
    padding: 0;
    background: var(--surface);
    color: var(--ink);
    border: 2px solid var(--line);
    border-radius: 0;
    box-shadow: 2px 2px 0 var(--shade);
    font-family: var(--font-body);
    font-size: 11px;
    line-height: 1;
    cursor: pointer;
  }

  .k:active {
    transform: translate(1px, 1px);
    box-shadow: 1px 1px 0 var(--shade);
  }

  .k.on {
    background: var(--ink);
    color: var(--surface);
  }

  .k.arrow {
    font-size: 14px;
  }
</style>
