<script lang="ts">
  import PixIcon from '../../ui/PixIcon.svelte';
  import type { FwSet } from '../../../api';
  import { tr as trs } from '../../../i18n';
  import Modal from '../../Modal.svelte';
  import Select from '../../ui/Select.svelte';
  import { parseAddresses, usedIn } from './fw-sets';

  let {
    sets,
    rules,
    onchange,
    onclose,
  }: {
    sets: FwSet[];
    /** Text of every rule of the draft, to show where a list is used. */
    rules: string[];
    onchange: (next: FwSet[]) => void;
    onclose: () => void;
  } = $props();

  let selected = $state<string>('');
  let newName = $state('');
  let newFamily = $state<'ipv4' | 'ipv6'>('ipv4');
  let addText = $state('');
  let nameError = $state<string | null>(null);
  let addError = $state<string | null>(null);
  let search = $state('');

  const NAME_RE = /^[a-z][a-z0-9_]{0,30}$/;

  const current = $derived(sets.find((s) => s.name === selected) ?? sets[0] ?? null);
  const shownElements = $derived.by(() => {
    if (!current) return [];
    const q = search.trim();
    return q ? current.elements.filter((e) => e.includes(q)) : current.elements;
  });
  const currentUses = $derived(current ? usedIn(rules, current.name) : 0);

  function replace(name: string, fn: (s: FwSet) => FwSet): void {
    onchange(sets.map((s) => (s.name === name ? fn(s) : s)));
  }

  function createList(): void {
    const name = newName.trim();
    if (!NAME_RE.test(name)) {
      nameError = 'fw.sets.errName';
      return;
    }
    if (sets.some((s) => s.name === name)) {
      nameError = 'fw.sets.errExists';
      return;
    }
    nameError = null;
    onchange([...sets, { name, family: newFamily, elements: [] }]);
    selected = name;
    newName = '';
  }

  function removeList(): void {
    if (!current || currentUses > 0) return;
    const name = current.name;
    onchange(sets.filter((s) => s.name !== name));
    selected = '';
  }

  function addAddresses(): void {
    if (!current) return;
    const res = parseAddresses(addText, current.family);
    if (res.bad.length) {
      addError = res.bad.slice(0, 3).join(', ');
      return;
    }
    addError = null;
    const known = new Set(current.elements);
    const fresh = res.items.filter((x) => !known.has(x));
    if (fresh.length) replace(current.name, (s) => ({ ...s, elements: [...s.elements, ...fresh] }));
    addText = '';
  }

  function removeElement(element: string): void {
    if (!current) return;
    replace(current.name, (s) => ({ ...s, elements: s.elements.filter((e) => e !== element) }));
  }

  function pick(name: string): void {
    selected = name;
    search = '';
    addError = null;
  }
</script>

<Modal title={$trs('fw.sets.title')} labelId="fw-sets-title" wide {onclose}>
  <p class="muted small">{$trs('fw.sets.help')}</p>
  <div class="sets">
    <div class="side stack">
      <ul class="list" aria-label={$trs('fw.sets.listLabel')}>
        {#each sets as s (s.name)}
          <li>
            <button class="item" class:on={current?.name === s.name} type="button" aria-pressed={current?.name === s.name} onclick={() => pick(s.name)}>
              <span class="mono nm">@{s.name}</span>
              <span class="small muted">{s.family === 'ipv6' ? 'IPv6' : 'IPv4'} · {s.elements.length}</span>
            </button>
          </li>
        {:else}
          <li class="muted small empty">{$trs('fw.sets.none')}</li>
        {/each}
      </ul>
      <form
        class="new"
        onsubmit={(e) => {
          e.preventDefault();
          createList();
        }}
      >
        <input
          class="input mono"
          type="text"
          autocomplete="off"
          spellcheck="false"
          placeholder={$trs('fw.sets.newName')}
          aria-label={$trs('fw.sets.newName')}
          bind:value={newName}
          oninput={() => (nameError = null)}
        />
        <Select size="sm" bind:value={newFamily} ariaLabel={$trs('fw.sets.family')} options={[{ value: 'ipv4', label: 'IPv4' }, { value: 'ipv6', label: 'IPv6' }]} />
        <button class="btn primary" type="submit">{$trs('fw.sets.create')}</button>
      </form>
      {#if nameError}<p class="error small" role="alert">{$trs(nameError)}</p>{/if}
    </div>

    <div class="main stack">
      {#if current}
        <div class="head">
          <h3 class="mono">@{current.name}</h3>
          <span class="small muted">
            {current.family === 'ipv6' ? 'IPv6' : 'IPv4'} ·
            {currentUses > 0 ? $trs('fw.sets.usedIn', { n: currentUses }) : $trs('fw.sets.unused')}
          </span>
          <button
            class="btn danger"
            type="button"
            disabled={currentUses > 0}
            title={currentUses > 0 ? $trs('fw.sets.deleteBlocked') : undefined}
            onclick={removeList}>{$trs('fw.sets.delete')}</button
          >
        </div>

        <form
          class="addrow"
          onsubmit={(e) => {
            e.preventDefault();
            addAddresses();
          }}
        >
          <input
            class="input mono"
            type="text"
            autocomplete="off"
            spellcheck="false"
            placeholder={current.family === 'ipv6' ? '2001:db8::/32' : '198.51.100.20, 10.20.0.0/24'}
            aria-label={$trs('fw.sets.addLabel')}
            bind:value={addText}
            oninput={() => (addError = null)}
          />
          <button class="btn primary" type="submit" disabled={!addText.trim()}>{$trs('fw.sets.add')}</button>
        </form>
        {#if addError}<p class="error small" role="alert">{$trs('fw.sets.errAddr', { items: addError })}</p>{/if}

        {#if current.elements.length > 8}
          <input class="input mono" type="search" placeholder={$trs('fw.sets.search')} aria-label={$trs('fw.sets.search')} bind:value={search} />
        {/if}

        <div class="chips" role="list">
          {#each shownElements as e (e)}
            <span class="chip2 mono" role="listitem">{e}<button type="button" aria-label={$trs('fw.sets.remove', { item: e })} onclick={() => removeElement(e)}><PixIcon name="x" size={12} /></button></span>
          {:else}
            <span class="muted small">{current.elements.length ? $trs('fw.sets.nothingFound') : $trs('fw.sets.emptyList')}</span>
          {/each}
        </div>
        <p class="small muted">{$trs('fw.sets.draftNote')}</p>
      {:else}
        <p class="muted">{$trs('fw.sets.pickOrCreate')}</p>
      {/if}
    </div>
  </div>
  <div class="row end">
    <button class="btn" type="button" onclick={onclose}>{$trs('common.close')}</button>
  </div>
</Modal>

<style>
  .sets {
    display: grid;
    grid-template-columns: minmax(180px, 234px) minmax(0, 1fr);
    gap: 14px;
    min-height: 234px;
  }

  .side,
  .main {
    min-width: 0;
  }

  .list {
    margin: 0;
    padding: 0;
    list-style: none;
    border: 2px solid var(--line);
    max-height: 234px;
    overflow: auto;
  }

  .list li + li {
    border-top: 2px dashed var(--seg-off);
  }

  .item {
    display: flex;
    justify-content: space-between;
    align-items: baseline;
    gap: 7px;
    width: 100%;
    padding: 7px 9px;
    border: 0;
    background: none;
    color: inherit;
    font: inherit;
    text-align: left;
    cursor: pointer;
  }

  .item.on {
    background: var(--ink);
    color: var(--surface);
  }

  .item.on .muted {
    color: inherit;
    opacity: 0.8;
  }

  .nm {
    overflow-wrap: anywhere;
  }

  .empty {
    padding: 9px;
  }

  .new {
    display: flex;
    flex-wrap: wrap;
    gap: 7px;
    align-items: center;
  }

  .new .input {
    flex: 1 1 100%;
    min-width: 0;
    height: 32px;
    padding-block: 0;
  }

  .new :global(.btn) {
    height: 32px;
    padding-block: 0;
    display: inline-flex;
    align-items: center;
  }

  .new :global(.sel) {
    height: 32px;
    padding-block: 0;
  }

  .head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 7px 11px;
  }

  .head h3 {
    margin: 0;
    font-size: 16px;
    overflow-wrap: anywhere;
  }

  .head :global(.btn) {
    margin-left: auto;
    height: 32px;
    padding-block: 0;
    display: inline-flex;
    align-items: center;
  }

  .addrow {
    display: flex;
    gap: 7px;
  }

  .addrow .input {
    flex: 1;
    min-width: 0;
    height: 32px;
    padding-block: 0;
  }

  .addrow :global(.btn) {
    height: 32px;
    padding-block: 0;
    display: inline-flex;
    align-items: center;
  }

  .chips {
    display: flex;
    flex-wrap: wrap;
    gap: 5px;
    max-height: 198px;
    overflow: auto;
    padding: 2px;
  }

  .chip2 {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    padding: 2px 4px 2px 7px;
    border: 2px solid var(--line);
    font-size: 12px;
  }

  .chip2 button {
    padding: 0 4px;
    border: 0;
    background: none;
    color: var(--muted);
    font: inherit;
    cursor: pointer;
  }

  .chip2 button:hover,
  .chip2 button:focus-visible {
    color: var(--crit);
  }

  @media (max-width: 640px) {
    .sets {
      grid-template-columns: minmax(0, 1fr);
    }
  }

  /* Phone: no nested scroll areas, 44 px targets, addresses as rows. */
  @media (max-width: 720px) {
    .sets {
      min-height: 0;
    }

    .list {
      max-height: none;
      overflow: visible;
      box-shadow: 3px 3px 0 var(--shade);
    }

    .item {
      align-items: center;
      min-height: 52px;
      padding: 7px 11px;
    }

    .new :global(.sel),
    .addrow .input,
    .addrow :global(.btn),
    .new .input,
    .new :global(.btn),
    .head :global(.btn) {
      height: 44px;
    }

    .chips {
      flex-direction: column;
      flex-wrap: nowrap;
      gap: 0;
      max-height: none;
      overflow: visible;
      padding: 0;
      border: 2px solid var(--line);
      box-shadow: 3px 3px 0 var(--shade);
    }

    .chip2 {
      justify-content: space-between;
      min-height: 48px;
      padding: 0 0 0 11px;
      border: 0;
      border-top: 2px dashed var(--seg-off);
      font-size: 13px;
      overflow-wrap: anywhere;
    }

    .chip2:first-child {
      border-top: 0;
    }

    .chip2 button {
      flex: none;
      width: 48px;
      height: 48px;
      padding: 0;
      font-size: 18px;
    }
  }
</style>
