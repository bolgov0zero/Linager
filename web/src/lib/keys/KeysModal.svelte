<script lang="ts">
  import { onMount } from 'svelte';
  import { tr as i18n, t } from '../../i18n';
  import Modal from '../Modal.svelte';
  import Select from '../ui/Select.svelte';
  import { showToast } from '../toast';
  import KeyEditModal from './KeyEditModal.svelte';
  import { LOCK_CHOICES, closeVault, createVault, lockMinutes, lockWaiting, openFile, removeEntry, saveFile, setLockMinutes, touch, vault } from './store';
  import { TYPES, bindText } from './labels';
  import { DEFAULT_FILE, FILE_EXT, PASSPHRASE_MIN, VaultError, accountOf, type Entry, type EntryType } from './vault';

  /** The window "Keys": create or open the personal vault, then list, add, change and delete the keys. */
  let { onclose }: { onclose: () => void } = $props();

  // ---- the vault is closed ----
  let p1 = $state('');
  let p2 = $state('');
  let createBusy = $state(false);
  let createError = $state('');

  let file = $state<File | null>(null);
  let openPass = $state('');
  let openBusy = $state(false);
  let openError = $state('');
  let fileInput = $state<HTMLInputElement | null>(null);

  const strength = $derived(p1.length < 8 ? 1 : p1.length < 12 ? 2 : p1.length < 16 ? 3 : 4);
  const createHint = $derived(
    p1.length > 0 && p1.length < PASSPHRASE_MIN ? $i18n('keys.create.short') : p2 && p1 !== p2 ? $i18n('keys.create.mismatch') : $i18n('keys.create.hint', { n: PASSPHRASE_MIN }),
  );
  const canCreate = $derived(p1.length >= PASSPHRASE_MIN && p1 === p2 && !createBusy);

  function explain(e: unknown): string {
    return e instanceof VaultError ? t(`keys.err.${e.code}`) : t('common.genericError');
  }

  async function create(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    if (!canCreate) return;
    createBusy = true;
    createError = '';
    try {
      await createVault(p1);
      showToast($i18n('keys.created', { name: DEFAULT_FILE }));
      p1 = p2 = '';
    } catch (e) {
      createError = explain(e);
    } finally {
      createBusy = false;
    }
  }

  async function open(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    if (!file || !openPass || openBusy) return;
    openBusy = true;
    openError = '';
    try {
      await openFile(file, openPass);
      openPass = '';
      file = null;
    } catch (e) {
      openError = explain(e);
    } finally {
      openBusy = false;
    }
  }

  // ---- the vault is open ----
  let filter = $state<'all' | EntryType>('all');
  let query = $state('');
  let deleting = $state<string | null>(null);
  let editing = $state<Entry | 'new' | null>(null);
  let closeAsk = $state(false);

  const entries = $derived($vault?.entries ?? []);
  const shown = $derived(
    entries
      .filter((e) => filter === 'all' || e.type === filter)
      .filter((e) => {
        const q = query.trim().toLowerCase();
        return !q || `${e.name} ${e.login} ${e.domain} ${bindText(e, $i18n)}`.toLowerCase().includes(q);
      })
      .sort((a, b) => a.name.localeCompare(b.name)),
  );

  const lockOptions = $derived(
    LOCK_CHOICES.map((m) => ({ value: String(m), label: m === 0 ? $i18n('keys.lock.off') : $i18n('keys.lock.m', { n: m }) })),
  );

  function save(): void {
    saveFile();
    showToast($i18n('keys.savedToast', { name: $vault?.file ?? '' }));
  }

  function closeVaultAsk(): void {
    if ($vault?.dirty) closeAsk = true;
    else closeVault();
  }

  function saveAndClose(): void {
    saveFile();
    closeAsk = false;
    closeVault();
  }

  onMount(() => touch());
</script>

<Modal title={$i18n('keys.title')} labelId="keys-title" {onclose} wide>
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="keys stack" onpointerdown={touch} onkeydown={touch}>
    {#if !$vault}
      <p class="muted small intro">{$i18n('keys.intro')}</p>
      <div class="cols">
        <form class="pane stack" onsubmit={create}>
          <h3>{$i18n('keys.create.title')}</h3>
          <label class="field">
            <span class="label">{$i18n('keys.pass')}</span>
            <input class="input" type="password" bind:value={p1} autocomplete="new-password" />
            <span class="meter" aria-hidden="true">
              {#each [1, 2, 3, 4] as n (n)}<i class:on={p1.length > 0 && n <= strength} class:w={strength === 3} class:c={strength < 3}></i>{/each}
            </span>
          </label>
          <label class="field">
            <span class="label">{$i18n('keys.pass2')}</span>
            <input class="input" type="password" bind:value={p2} autocomplete="new-password" />
          </label>
          <p class="warnbox small">{$i18n('keys.create.warn')}</p>
          <p class="small muted">{createHint}</p>
          {#if createError}<p class="error" role="alert">{createError}</p>{/if}
          <button class="btn primary" type="submit" disabled={!canCreate}>{createBusy ? $i18n('keys.create.busy') : $i18n('keys.create.go')}</button>
        </form>

        <form class="pane stack" onsubmit={open}>
          <h3>{$i18n('keys.open.title')}</h3>
          <div class="field">
            <span class="label">{$i18n('keys.file')}</span>
            <div class="row fileline">
              <span class="fn" class:none={!file}>{file?.name ?? $i18n('keys.file.none')}</span>
              <button class="btn" type="button" onclick={() => fileInput?.click()}>{$i18n('keys.file.pick')}</button>
              <input
                bind:this={fileInput}
                type="file"
                accept={FILE_EXT}
                hidden
                onchange={() => {
                  file = fileInput?.files?.[0] ?? null;
                  openError = '';
                }} />
            </div>
          </div>
          <label class="field">
            <span class="label">{$i18n('keys.pass')}</span>
            <input
              class="input"
              type="password"
              bind:value={openPass}
              autocomplete="current-password"
              oninput={() => (openError = '')} />
          </label>
          {#if openError}<p class="error" role="alert">{openError}</p>{/if}
          <button class="btn primary" type="submit" disabled={!file || !openPass || openBusy}>{openBusy ? $i18n('keys.open.busy') : $i18n('keys.open.go')}</button>
        </form>
      </div>
    {:else}
      <div class="strip">
        <span class="state"><i class="dot" aria-hidden="true"></i>{$i18n('keys.state.open')}</span>
        <strong class="fname" title={$vault.file}>{$vault.file}</strong>
        <span class="muted small">{$i18n('keys.count', { n: entries.length })}</span>
        {#if $vault.dirty}<span class="badge warn">{$i18n('keys.unsaved')}</span>{:else}<span class="muted small">{$i18n('keys.saved')}</span>{/if}
        <span class="lock small muted">
          {$i18n('keys.lock.label')}
          <Select size="sm" ariaLabel={$i18n('keys.lock.label')} value={String($lockMinutes)} options={lockOptions} onchange={(v) => setLockMinutes(Number(v))} />
        </span>
        <button class="btn" class:primary={$vault.dirty} type="button" onclick={save}>{$i18n('keys.save')}</button>
        <button class="btn" type="button" onclick={closeVaultAsk}>{$i18n('keys.close')}</button>
      </div>
      {#if $lockWaiting}<p class="warnbox small" role="status">{$i18n('keys.lock.waiting')}</p>{/if}

      <div class="row tools">
        <input class="input grow" type="search" placeholder={$i18n('keys.search')} aria-label={$i18n('keys.search')} bind:value={query} />
        <button class="chip" type="button" aria-pressed={filter === 'all'} onclick={() => (filter = 'all')}>{$i18n('keys.filter.all')}</button>
        {#each TYPES as ty (ty)}
          <button class="chip" type="button" aria-pressed={filter === ty} onclick={() => (filter = ty)}>{$i18n(`keys.type.${ty}`)}</button>
        {/each}
        <button class="btn primary end" type="button" onclick={() => (editing = 'new')}>{$i18n('keys.add')}</button>
      </div>

      {#if entries.length === 0}
        <p class="empty muted">{$i18n('keys.empty')}</p>
      {:else}
        <div class="wrap">
          <table class="tbl">
            <thead>
              <tr><th>{$i18n('keys.col.name')}</th><th>{$i18n('keys.col.use')}</th><th>{$i18n('keys.col.bind')}</th><th>{$i18n('keys.col.login')}</th><th></th></tr>
            </thead>
            <tbody>
              {#each shown as e (e.id)}
                <tr>
                  <td class="nm"><b>{e.name}</b></td>
                  <td><span class="badge ty-{e.type}">{$i18n(`keys.type.${e.type}`)}</span></td>
                  <td>{bindText(e, $i18n)}</td>
                  <td class="nw mono">{accountOf(e)}</td>
                  <td>
                    <div class="acts">
                      {#if deleting === e.id}
                        <span class="small error">{$i18n('keys.delete.ask')}</span>
                        <button class="btn sm danger" type="button" onclick={() => { removeEntry(e.id); deleting = null; }}>{$i18n('keys.yes')}</button>
                        <button class="btn sm" type="button" onclick={() => (deleting = null)}>{$i18n('keys.no')}</button>
                      {:else}
                        <button class="btn sm" type="button" onclick={() => (editing = e)}>{$i18n('keys.edit')}</button>
                        <button class="btn sm" type="button" onclick={() => (deleting = e.id)}>{$i18n('keys.delete')}</button>
                      {/if}
                    </div>
                  </td>
                </tr>
              {:else}
                <tr><td colspan="5" class="muted pad">{$i18n('keys.nomatch')}</td></tr>
              {/each}
            </tbody>
          </table>
        </div>
      {/if}
      <p class="small muted">{$i18n('keys.legend')}</p>
    {/if}
  </div>
</Modal>

{#if editing}
  <KeyEditModal entry={editing === 'new' ? null : editing} onclose={() => (editing = null)} />
{/if}

{#if closeAsk}
  <Modal title={$i18n('keys.closeAsk.title')} labelId="keys-close-title" onclose={() => (closeAsk = false)}>
    <p>{$i18n('keys.closeAsk.text')}</p>
    <div class="row end">
      <button class="btn" type="button" onclick={saveAndClose}>{$i18n('keys.closeAsk.save')}</button>
      <button
        class="btn danger"
        type="button"
        onclick={() => {
          closeAsk = false;
          closeVault();
        }}>{$i18n('keys.closeAsk.drop')}</button>
    </div>
  </Modal>
{/if}

<style>
  p,
  h3 {
    margin: 0;
  }

  .cols {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 14px;
    align-items: start;
  }

  .pane {
    padding: 14px;
    border: 2px solid var(--line);
  }

  .pane h3 {
    font-size: 13px;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--muted);
  }

  .intro {
    padding: 8px 10px;
    border: 2px dashed var(--seg-off);
  }

  .warnbox {
    padding: 8px 10px;
    border: 2px solid var(--warn);
    color: var(--warn);
  }

  .meter {
    display: flex;
    gap: 3px;
  }

  .meter i {
    width: 22px;
    height: 8px;
    background: var(--seg-off);
  }

  .meter i.on {
    background: var(--good);
  }

  .meter i.on.w {
    background: var(--warn);
  }

  .meter i.on.c {
    background: var(--crit);
  }

  .fileline {
    flex-wrap: nowrap;
  }

  .fn {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    padding: 6px 9px;
    border: 2px dashed var(--seg-off);
    font-size: 13px;
  }

  .fn.none {
    color: var(--muted);
  }

  .strip {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 10px;
    padding: 8px 12px;
    border: 2px solid var(--line);
  }

  .state {
    color: var(--good);
    display: inline-flex;
    align-items: center;
  }

  .dot {
    display: inline-block;
    width: 8px;
    height: 8px;
    margin-right: 6px;
    background: currentColor;
  }

  .fname {
    flex: 1 1 140px;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .lock {
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }

  .tools {
    gap: 8px;
  }

  .tools .grow {
    flex: 1 1 200px;
    max-width: 320px;
  }

  .tools .end {
    margin-left: auto;
  }

  .empty {
    padding: 22px;
    text-align: center;
    border: 2px dashed var(--seg-off);
  }

  .wrap {
    overflow-x: auto;
  }

  .tbl {
    width: 100%;
    min-width: 720px;
    border-collapse: collapse;
    font-size: 13px;
  }

  .tbl th {
    padding: 6px 8px;
    text-align: left;
    font-weight: 400;
    font-size: 12px;
    color: var(--muted);
    border-bottom: 2px solid var(--line);
    white-space: nowrap;
  }

  .tbl td {
    padding: 8px;
    vertical-align: middle;
    border-bottom: 1px dashed var(--seg-off);
  }

  .tbl tbody tr:hover td {
    background: var(--seg-off);
  }

  .nw {
    white-space: nowrap;
  }

  .pad {
    padding: 16px 8px;
  }

  .acts {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 6px;
  }

  .badge {
    display: inline-block;
    padding: 0 6px;
    border: 2px solid currentColor;
    font-size: 12px;
    line-height: 1.35;
    white-space: nowrap;
  }

  .badge.warn {
    color: var(--warn);
  }

  .ty-rdp {
    color: var(--accent);
  }

  .ty-uac {
    color: var(--warn);
  }

  .ty-any {
    color: var(--muted);
  }

  @media (max-width: 720px) {
    .cols {
      grid-template-columns: 1fr;
    }

    .tools .end {
      margin-left: 0;
      width: 100%;
    }
  }
</style>
