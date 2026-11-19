<script lang="ts">
  import PixIcon from '../ui/PixIcon.svelte';
  import { onMount } from 'svelte';
  import { api, ApiError, isUnauthorized, type Snippet } from '../../api';
  import { t, tr } from '../../i18n';
  import { showToast } from '../toast';
  import { loadSnippets, snippets } from '../snippets';
  import RowMenu from '../ui/RowMenu.svelte';
  import { isPhone } from '../ui/viewport';

  let { onauthlost }: { onauthlost: () => void } = $props();

  let editId = $state<number | 'new' | null>(null);
  let name = $state('');
  let command = $state('');
  let error = $state<string | null>(null);
  let busy = $state(false);
  let deleting = $state<Snippet | null>(null);

  onMount(() => void loadSnippets());

  function edit(sn: Snippet | null): void {
    editId = sn ? sn.id : 'new';
    name = sn?.name ?? '';
    command = sn?.command ?? '';
    error = null;
  }

  function fail(e: unknown): void {
    if (isUnauthorized(e)) onauthlost();
    else error = e instanceof ApiError && e.status === 422 ? (e.serverMessage ?? t('common.genericError')) : t('common.genericError');
  }

  async function save(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    if (busy || editId === null) return;
    busy = true;
    error = null;
    try {
      if (editId === 'new') await api.snippetCreate(name.trim(), command);
      else await api.snippetUpdate(editId, name.trim(), command);
      editId = null;
      showToast(t('snippets.saved'));
      await loadSnippets();
    } catch (e) {
      fail(e);
    } finally {
      busy = false;
    }
  }

  async function remove(): Promise<void> {
    const sn = deleting;
    if (!sn || busy) return;
    busy = true;
    try {
      await api.snippetDelete(sn.id);
      deleting = null;
      showToast(t('snippets.deleted'));
      await loadSnippets();
    } catch (e) {
      fail(e);
    } finally {
      busy = false;
    }
  }

  /** The row actions of a snippet in the sheet on phones. */
  function menu(sn: Snippet, i: number) {
    return [
      { key: 'edit', label: $tr('snippets.edit'), onselect: () => edit(sn) },
      { key: 'up', label: $tr('snippets.up'), disabled: i === 0, onselect: () => void move(i, -1) },
      { key: 'down', label: $tr('snippets.down'), disabled: i === $snippets.length - 1, onselect: () => void move(i, 1) },
      { key: 'delete', label: $tr('snippets.delete'), onselect: () => (deleting = sn) },
    ];
  }

  async function move(index: number, delta: number): Promise<void> {
    const list = [...$snippets];
    const j = index + delta;
    if (j < 0 || j >= list.length) return;
    [list[index], list[j]] = [list[j], list[index]];
    snippets.set(list.map((s, i) => ({ ...s, position: i })));
    try {
      await api.snippetsOrder(list.map((s) => s.id));
    } catch (e) {
      fail(e);
      await loadSnippets();
    }
  }
</script>

<div class="snippets">
  <header class="head">
    <h3>{$tr('snippets.title')}</h3>
    <button class="btn primary" type="button" onclick={() => edit(null)}>{$tr('snippets.add')}</button>
  </header>
  <p class="muted">{$tr('snippets.hint')}</p>

  {#if editId !== null}
    <form class="card stack" onsubmit={save}>
      <label class="field">
        <span class="label">{$tr('snippets.name')}</span>
        <input class="input" type="text" maxlength="80" required bind:value={name} />
      </label>
      <label class="field">
        <span class="label">{$tr('snippets.command')}</span>
        <textarea class="input mono" rows="4" required spellcheck="false" bind:value={command}></textarea>
      </label>
      {#if error}<p class="error" role="alert">{error}</p>{/if}
      <div class="row end">
        <button class="btn" type="button" onclick={() => (editId = null)}>{$tr('common.cancel')}</button>
        <button class="btn primary" type="submit" disabled={busy}>{$tr('snippets.save')}</button>
      </div>
    </form>
  {/if}

  {#if $snippets.length === 0 && editId === null}
    <p class="muted">{$tr('snippets.empty')}</p>
  {:else}
    <ul class="list">
      {#each $snippets as sn, i (sn.id)}
        <li class="item">
          <div class="text">
            <strong>{sn.name}</strong>
            <code class="mono small">{sn.command}</code>
          </div>
          {#if $isPhone}
            <RowMenu label={$tr('docker.menu')} title={sn.name} items={menu(sn, i)} />
          {:else}
          <div class="row acts">
            <button class="btn sm sq" type="button" aria-label={$tr('snippets.up')} title={$tr('snippets.up')} disabled={i === 0} onclick={() => void move(i, -1)}><PixIcon name="arrow-up" size={16} /></button>
            <button class="btn sm sq" type="button" aria-label={$tr('snippets.down')} title={$tr('snippets.down')} disabled={i === $snippets.length - 1} onclick={() => void move(i, 1)}><PixIcon name="arrow-down" size={16} /></button>
            <button class="btn sm" type="button" onclick={() => edit(sn)}>{$tr('snippets.edit')}</button>
            <button class="btn sm danger" type="button" onclick={() => (deleting = sn)}>{$tr('snippets.delete')}</button>
          </div>
          {/if}
          {#if deleting && deleting.id === sn.id}
            <div class="confirm stack">
              <p>{$tr('snippets.confirmDelete', { name: sn.name })}</p>
              <div class="row end">
                <button class="btn" type="button" onclick={() => (deleting = null)}>{$tr('common.cancel')}</button>
                <button class="btn danger" type="button" disabled={busy} onclick={() => void remove()}>{$tr('snippets.delete')}</button>
              </div>
            </div>
          {/if}
        </li>
      {/each}
    </ul>
  {/if}
</div>

<style>
  .snippets {
    display: flex;
    flex-direction: column;
    gap: 11px;
    min-width: 0;
  }

  .head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 11px;
  }

  h3,
  p {
    margin: 0;
  }

  textarea {
    resize: vertical;
    min-height: 72px;
  }

  .list {
    display: flex;
    flex-direction: column;
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .item {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 7px 11px;
    padding: 9px 0;
    border-bottom: 1px dashed var(--seg-off);
  }

  .text {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
    flex: 1 1 240px;
  }

  .text code {
    color: var(--muted);
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    max-height: 4.5em;
    overflow: hidden;
  }

  .acts {
    gap: 5px;
  }

  .confirm {
    flex: 1 0 100%;
    padding: 11px;
    border: 2px solid var(--crit);
  }

  /* Phones: every snippet is a card, its actions are in a sheet, the form buttons are one under another. */
  @media (max-width: 720px) {
    .list {
      gap: 11px;
    }

    .item {
      flex-wrap: nowrap;
      padding: 9px 5px 9px 11px;
      background: var(--surface);
      border: 2px solid var(--line);
      box-shadow: 3px 3px 0 var(--shade);
    }

    .text {
      flex: 1 1 0;
    }

    .confirm {
      margin-right: 5px;
    }

    .item:has(.confirm) {
      flex-wrap: wrap;
    }

    .snippets :global(.row.end) {
      flex-direction: column-reverse;
      align-items: stretch;
    }

    .snippets :global(.row.end > .btn) {
      width: 100%;
    }
  }
</style>
