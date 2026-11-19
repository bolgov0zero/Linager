<script lang="ts">
  import PixIcon from '../ui/PixIcon.svelte';
  import { tr } from '../../i18n';

  /**
   * On-screen keys for phones, five modes in a thin strip: main keys, Ctrl shortcuts, navigation, F1-F12 and symbols.
   * The arrows always stand on the right like on a real keyboard (up above down, left and right beside it).
   * Ctrl and Alt latch until the next key. Pressing a key must not take the focus away from the terminal, so the
   * soft keyboard stays where it is.
   */
  let {
    ctrl,
    alt,
    onkey,
    ontoggle,
  }: {
    ctrl: boolean;
    alt: boolean;
    /** A key name (Esc, Up, F5 ...), a single character or "ctrl:<letter>" for a ready shortcut. */
    onkey: (name: string) => void;
    ontoggle: (mod: 'ctrl' | 'alt') => void;
  } = $props();

  type Cell = { key: string; label: string; mod?: 'ctrl' | 'alt'; dim?: boolean } | null;

  const key = (k: string, label = k, dim = false): Cell => ({ key: k, label, dim });
  const chars = (s: string): Cell[] => [...s].map((c) => key(c));
  const shortcut = (c: string): Cell => key(`ctrl:${c}`, `^${c.toUpperCase()}`);

  const MODES: { id: string; tr: string; cells: Cell[] }[] = [
    {
      id: 'main',
      tr: 'term.keys.main',
      cells: [key('Esc'), key('Tab'), { key: 'Ctrl', label: 'Ctrl', mod: 'ctrl' }, { key: 'Alt', label: 'Alt', mod: 'alt' }, shortcut('c'), shortcut('d'), ...chars('|/-~`\\')],
    },
    { id: 'ctrl', tr: 'term.keys.ctrl', cells: ['c', 'd', 'z', 'l', 'r', 'a', 'e', 'u', 'w', 'k', 'x', 'o'].map(shortcut) },
    {
      id: 'nav',
      tr: 'term.keys.nav',
      cells: [key('Home'), key('End'), key('Ins', 'Ins', true), key('Del'), key('PgUp'), key('PgDn'), key('Esc'), key('Tab'), { key: 'Ctrl', label: 'Ctrl', mod: 'ctrl' }, { key: 'Alt', label: 'Alt', mod: 'alt' }, null, null],
    },
    { id: 'fn', tr: 'term.keys.fn', cells: Array.from({ length: 12 }, (_, i) => key(`F${i + 1}`, `F${i + 1}`, true)) },
    { id: 'sym', tr: 'term.keys.sym', cells: chars('$&*#{}[]<>_:') },
  ];

  // The arrow cluster: the inverted T of a real keyboard; Esc and Tab stand where PgUp and PgDn do on a laptop.
  const ARROWS: Cell[] = [key('Esc', 'Esc'), key('Up', '↑'), key('Tab', 'Tab'), key('Left', '←'), key('Down', '↓'), key('Right', '→')];
  const ARROW_ICON: Record<string, string> = { '←': 'arrow-left', '↑': 'arrow-up', '↓': 'arrow-down', '→': 'arrow-right' };
  const ARROW_NAMES: Record<string, string> = { '↑': 'Up', '↓': 'Down', '←': 'Left', '→': 'Right' };

  let mode = $state(0);
  const cells = $derived(MODES[mode]?.cells ?? []);

  /** Rows of the pad: six keys of the current mode, then three of the arrow cluster. */
  const rows = $derived([0, 1].map((r) => [...cells.slice(r * 6, r * 6 + 6), ...ARROWS.slice(r * 3, r * 3 + 3)]));

  const keep = (e: PointerEvent) => e.preventDefault();

  function press(cell: NonNullable<Cell>): void {
    if (cell.mod) ontoggle(cell.mod);
    else onkey(cell.key);
  }
</script>

<div class="keys" role="group" aria-label={$tr('term.keys')}>
  <div class="tabs" role="tablist">
    {#each MODES as m, i (m.id)}
      <button
        class="k tab"
        class:on={i === mode}
        type="button"
        role="tab"
        aria-selected={i === mode}
        onpointerdown={keep}
        onclick={() => (mode = i)}>{$tr(m.tr)}</button>
    {/each}
  </div>
  <div class="pad">
    {#each rows as row, r (r)}
      {#each row as cell, c (c)}
        {#if cell}
          {@const on = cell.mod === 'ctrl' ? ctrl : cell.mod === 'alt' ? alt : false}
          <button
            class="k"
            class:mod={!!cell.mod}
            class:on
            class:dim={cell.dim}
            class:arrow={!!ARROW_NAMES[cell.label]}
            class:small={c >= 6 && (cell.key === 'Esc' || cell.key === 'Tab')}
            type="button"
            aria-label={ARROW_NAMES[cell.label] ?? undefined}
            aria-pressed={cell.mod ? on : undefined}
            onpointerdown={keep}
            onclick={() => press(cell)}>{#if ARROW_ICON[cell.label]}<PixIcon name={ARROW_ICON[cell.label]} size={16} />{:else}{cell.label}{/if}</button>
        {:else}
          <span class="gap" aria-hidden="true"></span>
        {/if}
      {/each}
    {/each}
  </div>
</div>

<style>
  .keys {
    display: flex;
    flex-direction: column;
    gap: 4px;
    padding: 5px;
    background: var(--surface);
    border: 2px solid var(--line);
    user-select: none;
    -webkit-user-select: none;
    touch-action: manipulation;
  }

  .tabs {
    display: grid;
    grid-template-columns: repeat(5, minmax(0, 1fr));
    gap: 4px;
  }

  .pad {
    display: grid;
    grid-template-columns: repeat(9, minmax(0, 1fr));
    gap: 4px;
  }

  .k {
    min-width: 0;
    min-height: 44px;
    padding: 0 2px;
    background: var(--surface);
    color: var(--ink);
    border: 2px solid var(--line);
    border-radius: 0;
    font-family: var(--font-body);
    font-size: 12px;
    line-height: 1;
    cursor: pointer;
    box-shadow: 2px 2px 0 var(--shade);
  }

  .k:active {
    transform: translate(1px, 1px);
    box-shadow: 1px 1px 0 var(--shade);
  }

  .k.tab {
    min-height: 29px;
    font-size: 10px;
    white-space: nowrap;
  }

  .k.on {
    background: var(--ink);
    color: var(--surface);
  }

  .k.dim {
    color: var(--muted);
  }

  .k.on.dim {
    color: var(--surface);
  }

  .k.arrow {
    font-size: 14px;
  }

  .k.small {
    min-height: 44px;
    font-size: 10px;
  }
</style>
