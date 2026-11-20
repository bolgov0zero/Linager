<script lang="ts">
  import { tr } from '../../i18n';
  import PixIcon from '../ui/PixIcon.svelte';
  import { lockScroll } from '../ui/scrolllock';
  import { isPhone } from '../ui/viewport';
  import KeyUnlockModal from './KeyUnlockModal.svelte';
  import { loadDirectory, touch, vault } from './store';
  import { accountOf, sectionsFor, type Entry, type Known, type Sections, type Target } from './vault';

  /**
   * The list "Key" of a login form of a server: the keys of the open vault that fit it, this server's first. Picking one hands it to the form.
   * The list floats over the page like the project's Select (it is moved to <body>), so it does not make the form or the window taller.
   */
  let {
    serverId,
    use,
    variant = 'field',
    picked = null,
    onpick,
    onact,
    title = '',
  }: {
    serverId: string;
    use: 'rdp' | 'uac';
    /** "field": a labelled field of a form (a pick fills the form); "icon": a key button of a toolbar, each key with the buttons of what to type. */
    variant?: 'field' | 'icon';
    /** The name of the key the form was filled from, shown on the button. */
    picked?: string | null;
    onpick?: (entry: Entry) => void;
    /** The icon variant: what to type of the key (the login, the password, or the whole sign-in: login, Tab, password, Enter). */
    onact?: (entry: Entry, action: 'login' | 'password' | 'signin') => void;
    /** The tooltip and the name of the icon button. */
    title?: string;
  } = $props();

  const uid = `keys-${Math.random().toString(36).slice(2, 9)}`;

  let button = $state<HTMLButtonElement | null>(null);
  let open = $state(false);
  let unlocking = $state(false);
  let more = $state(false);
  let pos = $state({ left: 0, top: 0, width: 0, up: false, max: 280 });
  let target = $state<Target>({ serverId: '', groupId: null });
  let known = $state<Known>({ servers: new Set(), groups: new Set() });
  let serverName = $state('');
  let groupName = $state('');

  // Phone: the list is a sheet over the page; the page behind it must not scroll with the finger.
  $effect(() => {
    if (!open || !$isPhone) return;
    return lockScroll();
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
    const up = below < 180 && above > below;
    // a key button of a toolbar is narrow: its list is wider, and kept inside the window
    const width = variant === 'icon' ? Math.min(360, window.innerWidth - 16) : r.width;
    const left = variant === 'icon' ? Math.max(8, Math.min(r.left, window.innerWidth - width - 8)) : r.left;
    pos = { left, top: up ? r.top : r.bottom, width, up, max: Math.max(120, Math.min(300, up ? above : below)) };
  }

  async function show(): Promise<void> {
    place();
    open = true;
    more = false;
    touch();
    try {
      const dir = await loadDirectory();
      const here = dir.servers.find((s) => s.id === serverId);
      target = { serverId, groupId: here?.group_id ?? null };
      serverName = here?.name ?? '';
      groupName = dir.groups.find((g) => g.id === target.groupId)?.name ?? '';
      known = { servers: new Set(dir.servers.map((s) => s.id)), groups: new Set(dir.groups.map((g) => String(g.id))) };
    } catch {
      // the order is then by what is known: nothing is bound to this server, the keys fall under "for all" or "others"
    }
  }

  function hide(): void {
    open = false;
  }

  const sections = $derived<Sections>($vault ? sectionsFor($vault.entries, target, known, use) : { here: [], group: [], all: [], other: [] });
  const fitting = $derived(sections.here.length + sections.group.length + sections.all.length);

  function pick(entry: Entry): void {
    hide();
    button?.focus();
    touch();
    onpick?.(entry);
  }

  function act(entry: Entry, action: 'login' | 'password' | 'signin'): void {
    hide();
    touch();
    onact?.(entry, action);
  }

  function onkeydown(event: KeyboardEvent): void {
    if (open && event.key === 'Escape') {
      event.preventDefault();
      event.stopPropagation();
      hide();
      button?.focus();
    }
  }

  function onOutside(event: PointerEvent): void {
    if (!open) return;
    const target = event.target as Node;
    if (button?.contains(target) || document.getElementById(`${uid}-list`)?.contains(target)) return;
    hide();
  }

  function onScroll(event: Event): void {
    if ($isPhone && event.target === document) return;
    if (open && !document.getElementById(`${uid}-list`)?.contains(event.target as Node)) hide();
  }
</script>

<svelte:window onpointerdown={onOutside} onresize={hide} onscrollcapture={onScroll} {onkeydown} />

{#snippet item(e: Entry)}
  {#if variant === 'icon'}
    <li role="option" aria-selected="false" class="key act">
      <span class="nm" title={accountOf(e)}>{e.name}</span>
      <span class="btns">
        <button class="btn sm" type="button" title={$tr('keys.act.loginHint', { account: accountOf(e) })} onclick={() => act(e, 'login')}>{$tr('keys.act.login')}</button>
        <button class="btn sm" type="button" disabled={!e.password} title={$tr('keys.act.passwordHint')} onclick={() => act(e, 'password')}>{$tr('keys.act.password')}</button>
        <button class="btn sm primary" type="button" disabled={!e.password} title={$tr('keys.act.signinHint')} onclick={() => act(e, 'signin')}>{$tr('keys.act.signin')}</button>
      </span>
    </li>
  {:else}
  <li
    role="option"
    aria-selected="false"
    class="key"
    tabindex="0"
    onpointerdown={(ev) => ev.preventDefault()}
    onclick={() => pick(e)}
    onkeydown={(ev) => {
      if (ev.key === 'Enter' || ev.key === ' ') {
        ev.preventDefault();
        pick(e);
      }
    }}>
    <span class="nm">{e.name}</span>
    <small>{accountOf(e)}</small>
  </li>
  {/if}
{/snippet}

{#if variant === 'icon'}
  <button
    bind:this={button}
    class="btn sm ic"
    class:ink={open}
    type="button"
    role="combobox"
    aria-haspopup="listbox"
    aria-expanded={open}
    aria-controls={open ? `${uid}-list` : undefined}
    {title}
    aria-label={title}
    onclick={() => (open ? hide() : void show())}>
    <PixIcon name={$vault ? 'unlock' : 'key'} size={16} />
  </button>
{:else}
<div class="pick">
  <span class="cap">{$tr('keys.pick.label')}</span>
  <button
    bind:this={button}
    class="btn trig"
    type="button"
    role="combobox"
    aria-haspopup="listbox"
    aria-expanded={open}
    aria-controls={open ? `${uid}-list` : undefined}
    onclick={() => (open ? hide() : void show())}>
    <span class="cur">
      {#if picked}<PixIcon name="key" size={16} /><span class="val">{picked}</span>{:else}<span class="ph">{$tr('keys.pick.ph')}</span>{/if}
    </span>
    <span class="arrow" class:open aria-hidden="true"></span>
  </button>
</div>
{/if}

{#if open}
  <div use:portal class="sheet-scrim"></div>
  <ul
    use:portal
    id="{uid}-list"
    class="sel-list keylist"
    class:up={pos.up}
    role="listbox"
    aria-label={$tr('keys.pick.label')}
    style:left="{pos.left}px"
    style:top="{pos.top}px"
    style:width="{pos.width}px"
    style:max-height="{pos.max}px">
    {#if !$vault}
      <li class="note" role="presentation">
        <span class="small muted">{$tr('keys.pick.closed')}</span>
        <button
          class="btn sm primary"
          type="button"
          onclick={() => {
            hide();
            unlocking = true;
          }}>{$tr('keys.pick.open')}</button>
      </li>
    {:else}
      {#if sections.here.length}
        <li class="gh" role="presentation">{serverName ? $tr('keys.pick.here', { name: serverName }) : $tr('keys.pick.thisServer')}</li>
        {#each sections.here as e (e.id)}{@render item(e)}{/each}
      {/if}
      {#if sections.group.length}
        <li class="gh" role="presentation">{$tr('keys.pick.group', { name: groupName })}</li>
        {#each sections.group as e (e.id)}{@render item(e)}{/each}
      {/if}
      {#if sections.all.length}
        <li class="gh" role="presentation">{$tr('keys.pick.all')}</li>
        {#each sections.all as e (e.id)}{@render item(e)}{/each}
      {/if}
      {#if fitting === 0 && sections.other.length === 0}
        <li class="note small muted" role="presentation">{$tr('keys.pick.none')}</li>
      {/if}
      {#if sections.other.length}
        <li class="more" role="presentation">
          <button type="button" onclick={() => (more = !more)}>{more ? $tr('keys.pick.hideAll') : $tr('keys.pick.showAll', { n: sections.other.length })}</button>
        </li>
        {#if more}
          <li class="gh" role="presentation">{$tr('keys.pick.other')}</li>
          {#each sections.other as e (e.id)}{@render item(e)}{/each}
        {/if}
      {/if}
    {/if}
  </ul>
{/if}

{#if unlocking}
  <KeyUnlockModal
    onclose={() => (unlocking = false)}
    onopened={() => {
      unlocking = false;
      void show(); // the vault is open: the list of keys opens
    }} />
{/if}

<style>
  .pick {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .cap {
    font-size: 12px;
  }

  .trig {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 9px;
    width: 100%;
    box-shadow: none;
    padding: 7px 9px;
    background: var(--bg);
    font-family: var(--font-body);
    font-size: inherit;
    letter-spacing: 0;
    text-align: left;
  }

  .cur {
    display: flex;
    align-items: center;
    gap: 7px;
    min-width: 0;
  }

  .val,
  .ph {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .ph {
    color: var(--muted);
  }

  /* The same pixel chevron as the project's Select. */
  .arrow {
    flex: none;
    width: 6px;
    height: 6px;
    margin-top: -4px;
    border-right: 2px solid currentColor;
    border-bottom: 2px solid currentColor;
    transform: rotate(45deg);
  }

  .arrow.open {
    margin-top: 3px;
    transform: rotate(225deg);
  }

  /* The list is in <body>, so its rules are global (the base look comes from .sel-list). */
  :global(.keylist) {
    box-sizing: border-box;
  }

  :global(.keylist .gh) {
    padding: 6px 9px 2px;
    font-size: 11px;
    letter-spacing: 0.1em;
    text-transform: uppercase;
    color: var(--muted);
    white-space: normal;
    overflow-wrap: anywhere;
    cursor: default;
  }

  :global(.keylist li.key) {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 9px;
  }

  :global(.keylist li.key .nm) {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  :global(.keylist li.key small) {
    flex: none;
    max-width: 50%;
    overflow: hidden;
    text-overflow: ellipsis;
    color: var(--muted);
    font-size: 11px;
  }

  :global(.keylist li.key[aria-selected='false']:hover) {
    background: var(--seg-off);
  }

  /* the icon variant: a key with the buttons of what to type */
  :global(.keylist li.act) {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 9px;
    cursor: default;
  }

  :global(.keylist li.act .nm) {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  :global(.keylist li.act .btns) {
    display: flex;
    flex: none;
    gap: 5px;
  }

  :global(.keylist li.note) {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 7px;
    padding: 9px;
    white-space: normal;
    cursor: default;
  }

  :global(.keylist li.more) {
    padding: 0;
    border-top: 2px dashed var(--seg-off);
  }

  :global(.keylist li.more button) {
    width: 100%;
    padding: 6px 9px;
    background: none;
    border: 0;
    color: var(--accent);
    font: inherit;
    text-align: left;
    cursor: pointer;
  }
</style>
