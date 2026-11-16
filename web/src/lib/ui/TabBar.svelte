<script lang="ts">
  import PixIcon from './PixIcon.svelte';

  export interface TabBarItem {
    id: string;
    label: string;
    icon: string;
    current?: boolean;
    badge?: number | string;
    tone?: 'warn' | 'crit';
  }

  /** The bottom bar of the phone layout: up to five big targets, the current one inverted. Hidden on wide screens. */
  let { items, label, onselect }: { items: TabBarItem[]; label: string; onselect: (id: string) => void } = $props();
</script>

<nav class="tabbar" aria-label={label}>
  {#each items as it (it.id)}
    <button class="tab" type="button" aria-current={it.current ? 'page' : undefined} onclick={() => onselect(it.id)}>
      <PixIcon name={it.icon} />
      <span>{it.label}</span>
      {#if it.badge}<em class="cnt {it.tone ?? 'warn'}">{it.badge}</em>{/if}
    </button>
  {/each}
</nav>

<style>
  .tabbar {
    display: none;
  }

  @media (max-width: 720px) {
    .tabbar {
      position: fixed;
      left: 0;
      right: 0;
      bottom: 0;
      z-index: 15;
      display: grid;
      grid-auto-flow: column;
      grid-auto-columns: 1fr;
      gap: 4px;
      padding: 4px max(14px, env(safe-area-inset-right, 0px)) calc(4px + var(--safe-b)) max(14px, env(safe-area-inset-left, 0px));
      background: var(--surface);
      border-top: 2px solid var(--line);
    }

    .tab {
      position: relative;
      display: flex;
      flex-direction: column;
      align-items: center;
      justify-content: center;
      gap: 2px;
      min-width: 0;
      height: 54px;
      padding: 0;
      background: none;
      border: 2px solid transparent;
      color: var(--muted);
      cursor: pointer;
      font-family: var(--font-display);
      font-size: 12px;
    }

    .tab span {
      max-width: 100%;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }

    .tab[aria-current='page'] {
      background: var(--ink);
      color: var(--surface);
    }

    .cnt {
      position: absolute;
      top: 2px;
      left: calc(50% + 6px);
      min-width: 16px;
      height: 16px;
      padding: 0 3px;
      background: var(--warn);
      color: #fff;
      font-size: 11px;
      font-style: normal;
      line-height: 16px;
      text-align: center;
    }

    .cnt.crit {
      background: var(--crit);
    }
  }
</style>
