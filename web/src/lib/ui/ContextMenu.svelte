<script lang="ts" module>
  export interface MenuItem {
    key: string;
    label: string;
    disabled?: boolean;
    danger?: boolean;
    /** A thin line is drawn above this item. */
    separated?: boolean;
    onselect: () => void;
  }
</script>

<script lang="ts">
  import { onMount, tick } from 'svelte';

  /** A menu at a screen position (right click or a "more" button). It closes on any outside action. */
  let {
    x,
    y,
    items,
    label,
    onclose,
  }: {
    x: number;
    y: number;
    items: MenuItem[];
    label: string;
    onclose: () => void;
  } = $props();

  let el = $state<HTMLUListElement | undefined>(undefined);
  let active = $state(-1);
  let left = $state(0);
  let top = $state(0);

  onMount(() => {
    left = x;
    top = y;
    void tick().then(() => {
      if (!el) return;
      // Keep the whole menu on screen.
      const r = el.getBoundingClientRect();
      left = Math.max(4, Math.min(x, window.innerWidth - r.width - 4));
      top = Math.max(4, Math.min(y, window.innerHeight - r.height - 4));
      active = items.findIndex((i) => !i.disabled);
      el.focus({ preventScroll: true });
    });
    const close = () => onclose();
    window.addEventListener('resize', close);
    window.addEventListener('scroll', close, true);
    return () => {
      window.removeEventListener('resize', close);
      window.removeEventListener('scroll', close, true);
    };
  });

  function portal(node: HTMLElement) {
    document.body.appendChild(node);
    return { destroy: () => node.remove() };
  }

  function step(from: number, dir: 1 | -1): number {
    for (let i = 1; i <= items.length; i++) {
      const idx = (from + dir * i + items.length * 2) % items.length;
      if (!items[idx]?.disabled) return idx;
    }
    return from;
  }

  function pick(index: number): void {
    const item = items[index];
    if (!item || item.disabled) return;
    onclose();
    item.onselect();
  }

  function onkeydown(event: KeyboardEvent): void {
    if (event.key === 'Escape') {
      event.preventDefault();
      onclose();
    } else if (event.key === 'ArrowDown') {
      event.preventDefault();
      active = step(active, 1);
    } else if (event.key === 'ArrowUp') {
      event.preventDefault();
      active = step(active, -1);
    } else if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault();
      pick(active);
    } else if (event.key === 'Tab') {
      onclose();
    }
  }

  function onOutside(event: PointerEvent): void {
    if (el && !el.contains(event.target as Node)) onclose();
  }
</script>

<svelte:window onpointerdown={onOutside} />

<ul
  use:portal
  bind:this={el}
  class="sel-list sm ctx"
  role="menu"
  aria-label={label}
  tabindex="-1"
  style:left="{left}px"
  style:top="{top}px"
  {onkeydown}
  oncontextmenu={(e) => e.preventDefault()}
>
  {#each items as item, i (item.key)}
    <!-- Keyboard is handled on the list itself. -->
    <!-- svelte-ignore a11y_click_events_have_key_events -->
    <li
      role="menuitem"
      aria-disabled={item.disabled}
      class:active={i === active}
      class:off={item.disabled}
      class:danger={item.danger}
      class:sep={item.separated}
      onpointerenter={() => !item.disabled && (active = i)}
      onpointerdown={(e) => e.preventDefault()}
      onclick={() => pick(i)}
    >
      {item.label}
    </li>
  {/each}
</ul>

<style>
  :global(.sel-list.ctx) {
    margin: 0;
    min-width: 200px;
    outline: none;
    z-index: 50;
  }

  :global(.sel-list.ctx li.danger) {
    color: var(--crit);
  }

  :global(.sel-list.ctx li.sep) {
    border-top: 1px solid var(--seg-off);
    margin-top: 4px;
    padding-top: 8px;
  }

  :global(.sel-list li.off) {
    color: var(--muted);
    cursor: not-allowed;
    opacity: 0.55;
  }
</style>
