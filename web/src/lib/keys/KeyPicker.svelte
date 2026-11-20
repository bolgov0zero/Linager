<script lang="ts">
  import { tr } from '../../i18n';
  import PixIcon from '../ui/PixIcon.svelte';
  import { keysOpen, loadDirectory, touch, vault } from './store';
  import { accountOf, sectionsFor, type Entry, type Known, type Sections, type Target } from './vault';

  /** The list "Key" of a login form of a server: the keys of the open vault that fit it, this server's first. Picking one hands it to the form. */
  let {
    serverId,
    use,
    picked = null,
    onpick,
  }: {
    serverId: string;
    use: 'rdp' | 'uac';
    /** The name of the key the form was filled from, shown on the button. */
    picked?: string | null;
    onpick: (entry: Entry) => void;
  } = $props();

  let open = $state(false);
  let more = $state(false);
  let target = $state<Target>({ serverId: '', groupId: null });
  let known = $state<Known>({ servers: new Set(), groups: new Set() });
  let groupName = $state('');

  async function toggle(): Promise<void> {
    if (open) {
      open = false;
      return;
    }
    open = true;
    more = false;
    touch();
    try {
      const dir = await loadDirectory();
      target = { serverId, groupId: dir.servers.find((s) => s.id === serverId)?.group_id ?? null };
      groupName = dir.groups.find((g) => g.id === target.groupId)?.name ?? '';
      known = { servers: new Set(dir.servers.map((s) => s.id)), groups: new Set(dir.groups.map((g) => String(g.id))) };
    } catch {
      // the order is then by what is known: nothing is bound to this server, all keys fall under "for all" or "others"
    }
  }

  const sections = $derived<Sections>($vault ? sectionsFor($vault.entries, target, known, use) : { here: [], group: [], all: [], other: [] });
  const fitting = $derived(sections.here.length + sections.group.length + sections.all.length);

  function pick(entry: Entry): void {
    open = false;
    touch();
    onpick(entry);
  }
</script>

{#snippet item(e: Entry)}
  <button class="it" type="button" role="option" aria-selected={false} onclick={() => pick(e)}>
    <span class="nm">{e.name}</span>
    <small>{accountOf(e)}</small>
  </button>
{/snippet}

<div class="pick">
  <span class="cap">{$tr('keys.pick.label')}</span>
  <button class="btn trig" type="button" aria-haspopup="listbox" aria-expanded={open} onclick={() => void toggle()}>
    <span class="cur">
      {#if picked}<PixIcon name="key" size={16} /><span class="val">{picked}</span>{:else}<span class="ph">{$tr('keys.pick.ph')}</span>{/if}
    </span>
    <span class="chev" aria-hidden="true"></span>
  </button>
  {#if open}
    <div class="list" role="listbox" aria-label={$tr('keys.pick.label')}>
      {#if !$vault}
        <div class="closed">
          <span class="small muted">{$tr('keys.pick.closed')}</span>
          <button class="btn sm primary" type="button" onclick={() => { open = false; keysOpen.set(true); }}>{$tr('keys.pick.open')}</button>
        </div>
      {:else}
        {#if sections.here.length}<div class="gh">{$tr('keys.pick.here', { name: serverId })}</div>{#each sections.here as e (e.id)}{@render item(e)}{/each}{/if}
        {#if sections.group.length}<div class="gh">{$tr('keys.pick.group', { name: groupName })}</div>{#each sections.group as e (e.id)}{@render item(e)}{/each}{/if}
        {#if sections.all.length}<div class="gh">{$tr('keys.pick.all')}</div>{#each sections.all as e (e.id)}{@render item(e)}{/each}{/if}
        {#if fitting === 0 && sections.other.length === 0}<div class="closed small muted">{$tr('keys.pick.none')}</div>{/if}
        {#if sections.other.length}
          <button class="it more" type="button" onclick={() => (more = !more)}>{more ? $tr('keys.pick.hideAll') : $tr('keys.pick.showAll', { n: sections.other.length })}</button>
          {#if more}<div class="gh">{$tr('keys.pick.other')}</div>{#each sections.other as e (e.id)}{@render item(e)}{/each}{/if}
        {/if}
      {/if}
    </div>
  {/if}
</div>

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

  .chev {
    flex: none;
    width: 6px;
    height: 6px;
    margin-top: -4px;
    border-right: 2px solid currentColor;
    border-bottom: 2px solid currentColor;
    transform: rotate(45deg);
  }

  .trig[aria-expanded='true'] .chev {
    margin-top: 3px;
    transform: rotate(225deg);
  }

  /* inline, not floating: the form around it scrolls and would clip a floating list */
  .list {
    max-height: 220px;
    overflow-y: auto;
    border: 2px solid var(--line);
    background: var(--surface);
  }

  .gh {
    padding: 6px 9px 2px;
    font-size: 11px;
    letter-spacing: 0.1em;
    text-transform: uppercase;
    color: var(--muted);
  }

  .it {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 9px;
    width: 100%;
    padding: 6px 9px;
    background: none;
    border: 0;
    color: var(--ink);
    font: inherit;
    font-size: 13px;
    text-align: left;
    cursor: pointer;
  }

  .it:hover,
  .it:focus-visible {
    background: var(--seg-off);
  }

  .it .nm {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .it small {
    flex: none;
    color: var(--muted);
    font-size: 11px;
    max-width: 50%;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .it.more {
    color: var(--accent);
    border-top: 2px dashed var(--seg-off);
  }

  .closed {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 7px;
    padding: 9px;
  }
</style>
