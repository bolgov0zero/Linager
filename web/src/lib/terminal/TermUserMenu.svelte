<script lang="ts">
  import PixIcon from '../ui/PixIcon.svelte';
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
  class="btn sm um-btn"
  type="button"
  aria-haspopup="menu"
  aria-expanded={open}
  title={$tr('term.userHint')}
  {disabled}
  onclick={() => (open ? hide() : void show())}
>
  <span class="long">{$tr('term.user')}</span>
  <span class="short">{current || 'root'}</span>
  <span class="arrow" aria-hidden="true"></span>
</button>

{#if open}
  <div use:portal class="sheet-scrim"></div>
  <ul
    use:portal
    id="{uid}-list"
    class="list sel-list"
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
          <span class="mark" aria-hidden="true">{#if active}<PixIcon name="check" size={11} />{/if}</span>
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
    gap: 7px;
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
    min-width: 198px;
    max-width: min(378px, calc(100vw - 14px));
    margin: 0;
    padding: 4px 0;
    overflow-y: auto;
    list-style: none;
    background: var(--surface);
    color: var(--ink);
    border: 2px solid var(--line);
    box-shadow: 3px 3px 0 var(--shade);
  }

  .list li {
    padding: 0;
  }

  .list li.empty {
    padding: 4px 9px;
  }

  .item {
    display: flex;
    align-items: baseline;
    gap: 7px;
    width: 100%;
    padding: 4px 9px;
    border: 0;
    background: none;
    color: inherit;
    font: inherit;
    font-size: 11px;
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
    width: 11px;
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
    padding: 4px 9px;
    color: var(--muted);
    font-size: 11px;
  }

  /* Phones: the button shows the account the shell runs as, the list opens as a sheet from the bottom. */
  .short {
    display: none;
  }

  @media (max-width: 720px) {
    .list {
      min-width: 0;
      max-width: none;
      box-shadow: 0 -4px 0 var(--shade);
    }

    .long {
      display: none;
    }

    .short {
      display: inline;
      max-width: 96px;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }

    .item {
      min-height: 50px;
      align-items: center;
      padding: 0;
      font-size: 14px;
    }

    .empty {
      padding: 13px 0;
    }
  }
</style>
