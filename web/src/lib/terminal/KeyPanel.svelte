<script lang="ts">
  import { tr } from '../../i18n';

  /**
   * On-screen keys for phones: Esc, Tab, Ctrl and Alt (latched until the next key), arrows, navigation keys, F1-F12,
   * a few symbols that are awkward on a touch keyboard and common Ctrl shortcuts.
   * Pressing a key must not take the focus away from the terminal, so the soft keyboard stays where it is.
   */
  let {
    ctrl,
    alt,
    onkey,
    ontoggle,
  }: {
    ctrl: boolean;
    alt: boolean;
    /** A key name (Esc, Up, F5 ...) or a single character. */
    onkey: (name: string) => void;
    ontoggle: (mod: 'ctrl' | 'alt') => void;
  } = $props();

  const NAV = [
    ['Esc', 'Esc'], ['Tab', 'Tab'], ['Home', 'Home'], ['End', 'End'], ['PgUp', 'PgUp'], ['PgDn', 'PgDn'], ['Ins', 'Ins'], ['Del', 'Del'],
  ] as const;
  const ARROWS = [['Left', '←'], ['Down', '↓'], ['Up', '↑'], ['Right', '→']] as const;
  const SYMBOLS = ['|', '~', '`', '\\', '/', '-', '_', '$', '&', '*', '#', '{', '}', '[', ']', '<', '>'];
  const FKEYS = Array.from({ length: 12 }, (_, i) => `F${i + 1}`);
  const CTRL = [['c', 'Ctrl+C'], ['d', 'Ctrl+D'], ['z', 'Ctrl+Z'], ['l', 'Ctrl+L'], ['r', 'Ctrl+R'], ['a', 'Ctrl+A'], ['e', 'Ctrl+E'], ['u', 'Ctrl+U'], ['w', 'Ctrl+W']] as const;

  const keep = (e: PointerEvent) => e.preventDefault();
</script>

<div class="keys" role="group" aria-label={$tr('term.keys')}>
  <div class="line">
    <button class="k mod" class:on={ctrl} type="button" aria-pressed={ctrl} onpointerdown={keep} onclick={() => ontoggle('ctrl')}>Ctrl</button>
    <button class="k mod" class:on={alt} type="button" aria-pressed={alt} onpointerdown={keep} onclick={() => ontoggle('alt')}>Alt</button>
    {#each NAV as [name, label] (name)}
      <button class="k" type="button" onpointerdown={keep} onclick={() => onkey(name)}>{label}</button>
    {/each}
    {#each ARROWS as [name, label] (name)}
      <button class="k arrow" type="button" aria-label={name} onpointerdown={keep} onclick={() => onkey(name)}>{label}</button>
    {/each}
  </div>
  <div class="line">
    {#each FKEYS as f (f)}
      <button class="k" type="button" onpointerdown={keep} onclick={() => onkey(f)}>{f}</button>
    {/each}
  </div>
  <div class="line">
    {#each SYMBOLS as ch (ch)}
      <button class="k sym" type="button" onpointerdown={keep} onclick={() => onkey(ch)}>{ch}</button>
    {/each}
  </div>
  <div class="line">
    {#each CTRL as [ch, label] (ch)}
      <button class="k" type="button" onpointerdown={keep} onclick={() => onkey(`ctrl:${ch}`)}>{label}</button>
    {/each}
  </div>
</div>

<style>
  .keys {
    display: flex;
    flex-direction: column;
    gap: 4px;
    padding: 6px;
    background: var(--surface);
    border: 2px solid var(--line);
    user-select: none;
    -webkit-user-select: none;
    touch-action: manipulation;
  }

  .line {
    display: flex;
    gap: 4px;
    overflow-x: auto;
    scrollbar-width: none;
  }

  .line::-webkit-scrollbar {
    display: none;
  }

  .k {
    flex: 0 0 auto;
    min-width: 36px;
    padding: 6px 8px;
    background: var(--surface);
    color: var(--ink);
    border: 2px solid var(--line);
    border-radius: 0;
    font-family: var(--font-body);
    font-size: 13px;
    line-height: 1;
    cursor: pointer;
    box-shadow: 2px 2px 0 var(--shade);
  }

  .k:active {
    transform: translate(1px, 1px);
    box-shadow: 1px 1px 0 var(--shade);
  }

  .k.mod.on {
    background: var(--ink);
    color: var(--surface);
  }

  .k.arrow,
  .k.sym {
    min-width: 34px;
    font-size: 15px;
  }
</style>
