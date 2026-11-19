<script lang="ts">
  import { tick } from 'svelte';
  import PixIcon from './PixIcon.svelte';
  import { lockScroll } from './scrolllock';
  import { isPhone } from './viewport';

  /** A button that opens a menu of actions. The list is moved to <body> so scroll containers do not clip it. */
  let {
    label,
    items,
    disabled = false,
    icon,
    title,
    compact = false,
    iconSize = 18,
  }: {
    /** Text of the button; may be empty when `icon` stands for it (then `title` names the button). */
    label: string;
    /** Name of a pixel icon shown before the text. */
    icon?: string;
    /** Size of the icon in pixels: 18 keeps every square of the pixel icon at 2 px, 13.5 suits the 11 px text of a window header. */
    iconSize?: number;
    /** The small button (`btn sm`) of window headers and toolbars. */
    compact?: boolean;
    /** Tooltip and name of the button and of its list; defaults to the label. */
    title?: string;
    items: { key: string; label: string; disabled?: boolean; /** A check mark before the text: the chosen item. */ checked?: boolean; onselect: () => void }[];
    disabled?: boolean;
  } = $props();

  const uid = `am-${Math.random().toString(36).slice(2, 9)}`;

  let button = $state<HTMLButtonElement | null>(null);
  let open = $state(false);
  let active = $state(0);
  let pos = $state({ left: 0, top: 0, up: false, max: 280, right: false });

  // Phone: the list is a sheet over the page; the page behind it must not scroll with the finger.
  $effect(() => {
    if (!open || !$isPhone) return;
    return lockScroll();
  });

  function portal(node: HTMLElement) {
    document.body.appendChild(node);
    return { destroy: () => node.remove() };
  }

  function place(): void {
    if (!button) return;
    const r = button.getBoundingClientRect();
    const below = window.innerHeight - r.bottom - 8;
    const above = r.top - 8;
    const want = Math.min(280, items.length * 34 + 8);
    const up = below < want && above > below;
    // The menu is right-aligned with its button so it does not run off the table's edge.
    pos = { left: r.right, top: up ? r.top : r.bottom, up, max: Math.max(120, Math.min(280, up ? above : below)), right: true };
  }

  function firstEnabled(from: number, dir: 1 | -1): number {
    for (let i = 0; i < items.length; i++) {
      const idx = (from + dir * i + items.length * 2) % items.length;
      if (!items[idx]?.disabled) return idx;
    }
    return 0;
  }

  async function show(): Promise<void> {
    if (disabled || open) return;
    place();
    active = firstEnabled(0, 1);
    open = true;
    await tick();
  }

  function hide(refocus = false): void {
    open = false;
    if (refocus) button?.focus();
  }

  function pick(index: number): void {
    const item = items[index];
    if (!item || item.disabled) return;
    hide(true);
    item.onselect();
  }

  function onkeydown(event: KeyboardEvent): void {
    if (disabled) return;
    if (!open) {
      if (event.key === 'ArrowDown' || event.key === 'Enter' || event.key === ' ') {
        event.preventDefault();
        void show();
      }
      return;
    }
    if (event.key === 'ArrowDown') {
      event.preventDefault();
      active = firstEnabled(active + 1, 1);
    } else if (event.key === 'ArrowUp') {
      event.preventDefault();
      active = firstEnabled(active - 1, -1);
    } else if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault();
      pick(active);
    } else if (event.key === 'Escape' || event.key === 'Tab') {
      hide(event.key === 'Escape');
    }
  }

  function onOutside(event: PointerEvent): void {
    const target = event.target as Node;
    if (open && !button?.contains(target) && !document.getElementById(`${uid}-list`)?.contains(target)) hide();
  }
</script>

<svelte:window onpointerdown={onOutside} onresize={() => hide()} onscrollcapture={(e) => {
  if (open && !(e.target as Node).contains?.(document.getElementById(`${uid}-list`))) hide();
}} />

<button
  bind:this={button}
  class="btn am"
  class:sm={compact}
  type="button"
  aria-haspopup="menu"
  aria-expanded={open}
  aria-label={title}
  {title}
  aria-controls={open ? `${uid}-list` : undefined}
  {disabled}
  {onkeydown}
  onclick={() => (open ? hide() : void show())}
>
  {#if icon}<PixIcon name={icon} size={iconSize} />{/if}
  {label}
  <span class="arrow" aria-hidden="true"></span>
</button>

{#if open}
  <div use:portal class="sheet-scrim"></div>
  <ul
    use:portal
    id="{uid}-list"
    class="sel-list sm"
    class:up={pos.up}
    role="menu"
    aria-label={title ?? label}
    style:left="{pos.left}px"
    style:top="{pos.top}px"
    style:max-height="{pos.max}px"
    style:transform="translateX(-100%){pos.up ? ' translateY(-100%)' : ''}"
  >
    {#each items as item, i (item.key)}
      <!-- Keyboard is handled on the button; the items follow the active index. -->
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <li
        role="menuitem"
        aria-disabled={item.disabled}
        class:active={i === active && !item.disabled}
        class:off={item.disabled}
        onpointerenter={() => !item.disabled && (active = i)}
        onpointerdown={(e) => e.preventDefault()}
        onclick={() => pick(i)}
      >
        {#if item.checked}<span class="ck" aria-hidden="true"><PixIcon name="check" size={12} /></span>{/if}{item.label}
      </li>
    {/each}
  </ul>
{/if}

<style>
  .am {
    display: inline-flex;
    align-items: center;
    gap: 7px;
  }

  .ck {
    display: inline-flex;
    margin-right: 6px;
    vertical-align: middle;
  }

  .arrow {
    width: 0;
    height: 0;
    border-left: 4px solid transparent;
    border-right: 4px solid transparent;
    border-top: 5px solid currentColor;
  }

  :global(.sel-list li.off) {
    color: var(--muted);
    cursor: not-allowed;
    opacity: 0.55;
  }
</style>
