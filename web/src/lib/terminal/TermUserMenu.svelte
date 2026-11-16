<script lang="ts">
  import { tick } from 'svelte';
  import { api, type HostUser } from '../../api';
  import { tr } from '../../i18n';

  /**
   * "User" button with a list under it, like the snippets one: picks the account the shell of this window runs
   * as. The list of accounts is read from the server when the menu is opened; the list is moved to <body> so a
   * window or a scroll container does not clip it.
   */
  let {
    serverId,
    current,
    onpick,
    disabled = false,
  }: {
    serverId: string;
    /** The account of the shell now; empty for root. */
    current: string;
    onpick: (user: string) => void;
    disabled?: boolean;
  } = $props();

  const uid = `tu-${Math.random().toString(36).slice(2, 9)}`;

  let button = $state<HTMLButtonElement | null>(null);
  let open = $state(false);
  let pos = $state({ left: 0, top: 0, up: false, max: 320 });
  let users = $state<HostUser[] | null>(null);
  let failed = $state(false);

  // root first, then the regular accounts that have a shell
  const list = $derived((users ?? []).filter((u) => u.uid === 0 || (u.human && !u.no_shell)).sort((a, b) => (a.uid === 0 ? -1 : b.uid === 0 ? 1 : a.name.localeCompare(b.name))));

  function portal(node: HTMLElement) {
    document.body.appendChild(node);
    return { destroy: () => node.remove() };
  }

  async function load(): Promise<void> {
    failed = false;
    try {
      users = (await api.hostUsers(serverId)).users;
    } catch {
      failed = true;
    }
  }

  async function show(): Promise<void> {
    if (disabled || !button) return;
    void load();
    const r = button.getBoundingClientRect();
    const below = window.innerHeight - r.bottom - 8;
    const above = r.top - 8;
    const up = below < 200 && above > below;
    pos = { left: r.right, top: up ? r.top : r.bottom, up, max: Math.max(140, Math.min(360, up ? above : below)) };
    open = true;
    await tick();
  }

  function hide(): void {
    open = false;
  }

  function pick(u: HostUser): void {
    hide();
    const name = u.uid === 0 ? '' : u.name;
    if (name !== current) onpick(name);
  }

  function onOutside(event: PointerEvent): void {
    const target = event.target as Node;
    if (open && !button?.contains(target) && !document.getElementById(`${uid}-list`)?.contains(target)) hide();
  }
</script>

<svelte:window
  onpointerdown={onOutside}
  onresize={hide}
  onkeydown={(e) => e.key === 'Escape' && open && hide()}
  onscrollcapture={(e) => {
    if (open && !(e.target as Node).contains?.(document.getElementById(`${uid}-list`))) hide();
  }}
/>

<button
  bind:this={button}
  class="btn um-btn"
  type="button"
  aria-haspopup="menu"
  aria-expanded={open}
  title={$tr('term.userHint')}
  {disabled}
  onclick={() => (open ? hide() : void show())}
>
  {$tr('term.user')}
  <span class="arrow" aria-hidden="true"></span>
</button>

{#if open}
  <ul
    use:portal
    id="{uid}-list"
    class="list"
    role="menu"
    aria-label={$tr('term.user')}
    style:left="{pos.left}px"
    style:top="{pos.top}px"
    style:max-height="{pos.max}px"
    style:transform="translateX(-100%){pos.up ? ' translateY(-100%)' : ''}"
  >
    {#each list as u (u.name)}
      {@const active = (u.uid === 0 ? '' : u.name) === current}
      <li role="none">
        <button class="item" class:active type="button" role="menuitemradio" aria-checked={active} onclick={() => pick(u)}>
          <span class="mark" aria-hidden="true">{active ? '✓' : ''}</span>
          <span class="name">{u.name}</span>
          {#if u.gecos}<span class="muted small gecos">{u.gecos}</span>{/if}
        </button>
      </li>
    {:else}
      <li class="empty" role="none">{failed ? $tr('term.userFailed') : users === null ? $tr('common.loading') : $tr('term.userNone')}</li>
    {/each}
  </ul>
{/if}

<style>
  .um-btn {
    display: inline-flex;
    align-items: center;
    gap: 8px;
  }

  .arrow {
    width: 0;
    height: 0;
    border-left: 4px solid transparent;
    border-right: 4px solid transparent;
    border-top: 5px solid currentColor;
  }

  .list {
    position: fixed;
    z-index: 100001;
    min-width: 220px;
    max-width: min(420px, calc(100vw - 16px));
    margin: 0;
    padding: 4px;
    overflow-y: auto;
    list-style: none;
    background: var(--surface);
    color: var(--ink);
    border: 2px solid var(--line);
    box-shadow: 3px 3px 0 var(--shade);
  }

  .item {
    display: flex;
    align-items: baseline;
    gap: 8px;
    width: 100%;
    padding: 8px 10px;
    border: 0;
    background: none;
    color: inherit;
    font: inherit;
    font-size: 13px;
    text-align: left;
    cursor: pointer;
  }

  .item:hover,
  .item:focus-visible {
    background: var(--ink);
    color: var(--surface);
  }

  .item:hover .muted,
  .item:focus-visible .muted {
    color: inherit;
  }

  .mark {
    flex: none;
    width: 12px;
  }

  .name {
    font-weight: 600;
  }

  .gecos {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .empty {
    padding: 8px 10px;
    color: var(--muted);
    font-size: 13px;
  }
</style>
