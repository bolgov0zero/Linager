<script lang="ts" generics="T extends string | number">
  import { tick } from 'svelte';
  import { locale } from '../../i18n';
  import { lockScroll } from './scrolllock';
  import { isPhone } from './viewport';

  // Project-styled replacement for <select>: a button with the current value and a listbox.
  // The list is moved to <body> and positioned with `fixed`, so table and scroll containers do not clip it
  // and a surrounding <label> does not turn clicks on options into clicks on the button.
  let {
    value = $bindable(),
    options,
    onchange,
    size = 'md',
    width,
    ariaLabel,
    disabled = false,
  }: {
    value: T;
    options: { value: T; label: string }[];
    onchange?: (value: T) => void;
    /** md: like `.input` (fills its container); sm: compact control for toolbars and table rows. */
    size?: 'md' | 'sm';
    /** CSS width of the control; md defaults to 100%, sm to its content. */
    width?: string;
    ariaLabel?: string;
    disabled?: boolean;
  } = $props();

  const uid = `sel-${Math.random().toString(36).slice(2, 9)}`;

  let button = $state<HTMLButtonElement | null>(null);
  let open = $state(false);
  let active = $state(0);
  let pos = $state({ left: 0, top: 0, width: 0, up: false, max: 280 });
  let typed = '';
  let typedAt = 0;

  const current = $derived(options.find((o) => o.value === value));

  // Phone: the list is a sheet over the page; the page behind it must not scroll with the finger.
  $effect(() => {
    if (!open || !$isPhone) return;
    return lockScroll();
  });

  // A combobox button does not take its name from a wrapping <label>; reuse the label's caption instead.
  let labelText = $state<string | undefined>(undefined);
  $effect(() => {
    void $locale; // re-read after a language switch (the caption is rendered first)
    const caption = button?.closest('label')?.firstElementChild;
    labelText = caption && caption !== button ? caption.textContent?.trim() || undefined : undefined;
  });

  /** Moves the node to <body> for the lifetime of the element. */
  function portal(node: HTMLElement) {
    document.body.appendChild(node);
    return { destroy: () => node.remove() };
  }

  function place(): void {
    if (!button) return;
    const r = button.getBoundingClientRect();
    const below = window.innerHeight - r.bottom - 8;
    const above = r.top - 8;
    const want = Math.min(280, options.length * 34 + 8);
    const up = below < want && above > below;
    pos = { left: r.left, top: up ? r.top : r.bottom, width: r.width, up, max: Math.max(120, Math.min(280, up ? above : below)) };
  }

  async function show(): Promise<void> {
    if (disabled || open) return;
    active = Math.max(0, options.findIndex((o) => o.value === value));
    place();
    open = true;
    await tick();
    document.getElementById(`${uid}-${active}`)?.scrollIntoView({ block: 'nearest' });
  }

  function hide(): void {
    open = false;
  }

  function pick(index: number): void {
    const option = options[index];
    hide();
    button?.focus();
    if (!option || option.value === value) return;
    value = option.value;
    onchange?.(option.value);
  }

  function move(to: number): void {
    active = Math.min(Math.max(to, 0), options.length - 1);
    document.getElementById(`${uid}-${active}`)?.scrollIntoView({ block: 'nearest' });
  }

  /** Type-ahead: jumps to the next option starting with the typed letters. */
  function typeahead(key: string): void {
    const now = Date.now();
    typed = now - typedAt > 700 ? key : typed + key;
    typedAt = now;
    const start = open ? active : options.findIndex((o) => o.value === value);
    const n = options.length;
    for (let i = 1; i <= n; i++) {
      // A new letter searches from the next option; a longer prefix may stay on the current one.
      const idx = (start + (typed.length > 1 ? i - 1 : i) + n) % n;
      if (options[idx].label.toLowerCase().startsWith(typed.toLowerCase())) {
        if (open) move(idx);
        else pick(idx);
        return;
      }
    }
  }

  function onkeydown(event: KeyboardEvent): void {
    if (disabled) return;
    const key = event.key;
    if (!open) {
      if (key === 'ArrowDown' || key === 'ArrowUp' || key === 'Enter' || key === ' ') {
        event.preventDefault();
        void show();
      } else if (key.length === 1 && !event.ctrlKey && !event.metaKey && !event.altKey) {
        typeahead(key);
      }
      return;
    }
    switch (key) {
      case 'ArrowDown':
        event.preventDefault();
        move(active + 1);
        break;
      case 'ArrowUp':
        event.preventDefault();
        move(active - 1);
        break;
      case 'Home':
        event.preventDefault();
        move(0);
        break;
      case 'End':
        event.preventDefault();
        move(options.length - 1);
        break;
      case 'Enter':
      case ' ':
        event.preventDefault();
        pick(active);
        break;
      case 'Escape':
        // Keep a surrounding modal open: only the list closes.
        event.preventDefault();
        event.stopPropagation();
        hide();
        break;
      case 'Tab':
        hide();
        break;
      default:
        if (key.length === 1 && !event.ctrlKey && !event.metaKey && !event.altKey) typeahead(key);
    }
  }

  function onOutside(event: PointerEvent): void {
    if (!open) return;
    const target = event.target as Node;
    if (button?.contains(target) || document.getElementById(`${uid}-list`)?.contains(target)) return;
    hide();
  }

  function onScroll(event: Event): void {
    // Phone: pinning the page (lockScroll) fires a scroll event on the document, which must not close the sheet.
    if ($isPhone && event.target === document) return;
    if (open && !document.getElementById(`${uid}-list`)?.contains(event.target as Node)) hide();
  }
</script>

<svelte:window onpointerdown={onOutside} onresize={hide} onscrollcapture={onScroll} />

<button
  bind:this={button}
  class="sel {size}"
  class:open
  type="button"
  role="combobox"
  aria-haspopup="listbox"
  aria-expanded={open}
  aria-controls={open ? `${uid}-list` : undefined}
  aria-activedescendant={open ? `${uid}-${active}` : undefined}
  aria-label={ariaLabel ?? labelText}
  style:width={width ?? (size === 'md' ? '100%' : undefined)}
  {disabled}
  onclick={() => (open ? hide() : void show())}
  {onkeydown}
>
  <span class="text">{current?.label ?? ''}</span>
  <span class="arrow" aria-hidden="true"></span>
</button>

{#if open}
  <div use:portal class="sheet-scrim"></div>
  <ul
    use:portal
    id="{uid}-list"
    class="sel-list {size}"
    class:up={pos.up}
    role="listbox"
    aria-label={ariaLabel ?? labelText}
    style:left="{pos.left}px"
    style:top="{pos.top}px"
    style:min-width="{pos.width}px"
    style:max-height="{pos.max}px"
  >
    {#each options as option, i (option.value)}
      <!-- Keyboard is handled on the combobox button (aria-activedescendant), as in the ARIA pattern. -->
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <li
        id="{uid}-{i}"
        role="option"
        aria-selected={option.value === value}
        class:active={i === active}
        onpointerenter={() => (active = i)}
        onpointerdown={(e) => e.preventDefault()}
        onclick={() => pick(i)}
      >
        {option.label}
      </li>
    {/each}
  </ul>
{/if}

<style>
  .sel {
    display: inline-flex;
    align-items: center;
    justify-content: space-between;
    gap: 9px;
    max-width: 100%;
    min-width: 0;
    background: var(--surface);
    color: var(--ink);
    border: 2px solid var(--line);
    border-radius: 0;
    font: inherit;
    text-align: left;
    cursor: pointer;
  }

  .sel.md {
    padding: 7px 9px;
  }

  .sel.sm {
    padding: 5px 7px;
    font-size: 12px;
  }

  .sel:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .sel:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }

  .text {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  /* Pixel chevron: a small square-cornered "v" drawn with two borders. */
  .arrow {
    flex: none;
    width: 6px;
    height: 6px;
    margin-top: -4px;
    border-right: 2px solid currentColor;
    border-bottom: 2px solid currentColor;
    transform: rotate(45deg);
  }

  .sel.open .arrow {
    margin-top: 3px;
    transform: rotate(225deg);
  }

  :global(.sel-list) {
    position: fixed;
    z-index: 40;
    margin: 4px 0 0;
    padding: 4px 0;
    overflow-y: auto;
    list-style: none;
    background: var(--surface);
    color: var(--ink);
    border: 2px solid var(--line);
    box-shadow: 3px 3px 0 var(--shade);
    font-family: var(--font-body);
  }

  :global(.sel-list.up) {
    margin: 0 0 4px;
    transform: translateY(-100%);
  }

  :global(.sel-list.sm) {
    font-size: 12px;
  }

  :global(.sel-list li) {
    padding: 5px 9px;
    white-space: nowrap;
    cursor: pointer;
  }

  :global(.sel-list li.active) {
    background: var(--seg-off);
  }

  :global(.sel-list li[aria-selected='true']) {
    background: var(--ink);
    color: var(--surface);
  }

  /* Phone: a touch-sized control. */
  @media (max-width: 720px) {
    .sel {
      min-height: 44px;
    }
  }
</style>
