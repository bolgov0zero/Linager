<script lang="ts">
  import { get } from 'svelte/store';
  import { untrack } from 'svelte';
  import { api, type FileEntry, type ServerDetail } from '../../../api';
  import { locale, t, tr as tStore, type Vars } from '../../../i18n';
  import Modal from '../../Modal.svelte';
  import { showToast } from '../../toast';
  import { formatBytes, formatDateTime } from '../format';
  import {
    crumbs,
    errorText,
    isStatus,
    joinPath,
    nameError,
    openErrorText,
    parentPath,
    sortEntries,
    unavailable,
  } from './files-util';

  let { server }: { server: ServerDetail } = $props();

  type LoadState = 'loading' | 'ready' | 'offline' | 'disabled' | 'error';

  interface Editor {
    path: string;
    name: string;
    text: string;
    saved: string;
    mtime: number;
    askClose: boolean;
    conflict: boolean;
    saving: boolean;
  }

  interface Ask {
    title: string;
    text: string;
    label: string;
    run: () => Promise<void>;
  }

  // Only the id drives loading. Other fields of `server` change in place on live events
  // and must not reload the listing or restart anything.
  const serverId = $derived(server.id);

  let path = $state('/');
  let listing = $state<FileEntry[] | null>(null);
  let loadState = $state<LoadState>('loading');
  let loadError = $state('');
  let seq = 0;

  let prompt = $state<{ kind: 'dir' | 'file'; name: string; error: string; busy: boolean } | null>(null);
  let ask = $state<Ask | null>(null);
  let askBusy = $state(false);
  let editor = $state<Editor | null>(null);
  let upload = $state<{ name: string; index: number; total: number; fraction: number } | null>(null);
  let picker = $state<HTMLInputElement | undefined>(undefined);

  const offline = $derived(loadState === 'offline' || !server.online);
  const readOnly = $derived(offline || loadState === 'disabled');
  const dirty = $derived(editor !== null && editor.text !== editor.saved);

  function T(key: string, vars?: Vars): string {
    return t(key, get(locale), vars);
  }

  function apply(res: { path: string; entries: FileEntry[] }): void {
    path = res.path;
    listing = sortEntries(res.entries);
    loadState = 'ready';
  }

  /** Loads a directory. A failed navigation keeps the current listing and shows a toast. */
  async function go(dir: string, id: string = serverId): Promise<void> {
    const req = ++seq;
    try {
      const res = await api.files(id, dir);
      if (req !== seq) return;
      apply(res);
    } catch (e) {
      if (req !== seq) return;
      const kind = unavailable(e);
      if (kind) {
        loadState = kind;
        return;
      }
      if (listing) {
        showToast(errorText(e, T));
      } else {
        loadState = 'error';
        loadError = errorText(e, T);
      }
    }
  }

  /** Opens /root when the server has it, otherwise the filesystem root. */
  async function start(id: string): Promise<void> {
    const req = ++seq;
    listing = null;
    loadState = 'loading';
    path = '/';
    let res: { path: string; entries: FileEntry[] } | null = null;
    try {
      res = await api.files(id, '/root');
    } catch {
      res = null;
    }
    if (req !== seq) return;
    if (res) {
      apply(res);
      return;
    }
    await go('/', id);
  }

  $effect(() => {
    const id = serverId;
    untrack(() => void start(id));
  });

  async function onPick(event: Event): Promise<void> {
    const input = event.currentTarget as HTMLInputElement;
    const files = Array.from(input.files ?? []);
    input.value = '';
    if (files.length === 0) return;
    const id = serverId;
    const dir = path;
    for (const [i, file] of files.entries()) {
      upload = { name: file.name, index: i + 1, total: files.length, fraction: 0 };
      try {
        await api.uploadFile(id, dir, file, (fraction) => {
          if (upload) upload.fraction = fraction;
        });
      } catch (e) {
        showToast(T('files.upload.fail', { name: file.name, error: errorText(e, T) }));
        break;
      }
    }
    upload = null;
    if (path === dir) await go(dir, id);
  }

  async function submitPrompt(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    const current = prompt;
    if (!current) return;
    const key = nameError(current.name);
    if (key) {
      current.error = T(key);
      return;
    }
    const name = current.name.trim();
    const kind = current.kind;
    const id = serverId;
    const full = joinPath(path, name);
    current.busy = true;
    current.error = '';
    try {
      if (kind === 'dir') await api.fileMkdir(id, full);
      else await api.fileCreate(id, full);
      prompt = null;
      showToast(T('files.created', { name }));
      await go(path, id);
    } catch (e) {
      current.busy = false;
      current.error = errorText(e, T);
    }
  }

  function requestDelete(entry: FileEntry): void {
    const id = serverId;
    const full = joinPath(path, entry.name);
    const isDir = entry.type === 'dir';
    ask = {
      title: T('files.deleteTitle'),
      text: T(isDir ? 'files.deleteDir' : 'files.deleteFile', { path: full }),
      label: T('files.delete'),
      run: async () => {
        try {
          await api.fileDelete(id, full);
          if (editor?.path === full) editor = null;
          showToast(T('files.deleted', { name: entry.name }));
          await go(path, id);
        } catch (e) {
          showToast(errorText(e, T));
        }
      },
    };
  }

  async function confirmAsk(): Promise<void> {
    const current = ask;
    if (!current || askBusy) return;
    askBusy = true;
    try {
      await current.run();
    } finally {
      askBusy = false;
      ask = null;
    }
  }

  async function openFile(entry: FileEntry): Promise<void> {
    const full = joinPath(path, entry.name);
    try {
      const f = await api.fileContent(serverId, full);
      editor = {
        path: full,
        name: entry.name,
        text: f.content,
        saved: f.content,
        mtime: f.mtime,
        askClose: false,
        conflict: false,
        saving: false,
      };
    } catch (e) {
      showToast(openErrorText(e, T));
    }
  }

  function closeEditor(): void {
    if (!editor) return;
    if (editor.text !== editor.saved) {
      editor.askClose = true;
      return;
    }
    editor = null;
  }

  async function saveEditor(): Promise<void> {
    const ed = editor;
    if (!ed || ed.saving) return;
    const sent = ed.text;
    ed.saving = true;
    try {
      const res = await api.fileSave(serverId, ed.path, sent, ed.mtime);
      ed.saved = sent;
      ed.mtime = res.mtime;
      showToast(T('files.saved'));
    } catch (e) {
      if (isStatus(e, 409)) ed.conflict = true;
      else showToast(errorText(e, T));
    } finally {
      ed.saving = false;
    }
  }

  async function reloadEditor(): Promise<void> {
    const ed = editor;
    if (!ed) return;
    try {
      const f = await api.fileContent(serverId, ed.path);
      if (editor !== ed) return;
      ed.text = f.content;
      ed.saved = f.content;
      ed.mtime = f.mtime;
      ed.conflict = false;
      ed.askClose = false;
    } catch (e) {
      showToast(openErrorText(e, T));
    }
  }

  function kindLabel(type: FileEntry['type']): string {
    return T(`files.kind.${type}`);
  }
</script>

<div class="files">
  <div class="toolbar">
    <nav class="crumbs" aria-label={$tStore('files.pathLabel')}>
      <button
        class="crumb"
        type="button"
        aria-current={path === '/' ? 'page' : undefined}
        onclick={() => go('/')}>/</button
      >
      {#each crumbs(path) as c (c.path)}
        <span class="sep" aria-hidden="true">›</span>
        <button
          class="crumb"
          type="button"
          aria-current={c.path === path ? 'page' : undefined}
          onclick={() => go(c.path)}>{c.name}</button
        >
      {/each}
    </nav>

    <div class="row actions">
      <button class="btn" type="button" disabled={path === '/' || !listing} onclick={() => go(parentPath(path))}>
        {$tStore('files.up')}
      </button>
      <button class="btn" type="button" disabled={readOnly || !listing} onclick={() => (prompt = { kind: 'dir', name: '', error: '', busy: false })}>
        {$tStore('files.newFolder')}
      </button>
      <button class="btn" type="button" disabled={readOnly || !listing} onclick={() => (prompt = { kind: 'file', name: '', error: '', busy: false })}>
        {$tStore('files.newFile')}
      </button>
      <button class="btn" type="button" disabled={readOnly || !listing || upload !== null} onclick={() => picker?.click()}>
        {$tStore('files.upload')}
      </button>
      <button class="btn" type="button" disabled={!listing} onclick={() => go(path)}>{$tStore('files.refresh')}</button>
      <input class="picker" bind:this={picker} type="file" multiple tabindex="-1" aria-hidden="true" onchange={onPick} />
    </div>
  </div>

  {#if offline}
    <p class="notice" role="status">{$tStore('files.offline')}</p>
  {:else if loadState === 'disabled'}
    <p class="notice" role="status">{$tStore('files.disabled')}</p>
  {/if}

  {#if upload}
    <div class="upload" role="status">
      <p class="small mono">{$tStore('files.uploading', { name: upload.name, index: upload.index, total: upload.total })}</p>
      <div class="bar" role="progressbar" aria-valuemin="0" aria-valuemax="100" aria-valuenow={Math.round(upload.fraction * 100)}>
        <span style="width: {upload.fraction * 100}%"></span>
      </div>
    </div>
  {/if}

  {#if !listing}
    {#if loadState === 'error'}
      <div class="state">
        <p class="error" role="alert">{$tStore('files.loadFailed')}: {loadError}</p>
        <div class="row">
          <button class="btn" type="button" onclick={() => go(path)}>{$tStore('files.retry')}</button>
        </div>
      </div>
    {:else if loadState === 'loading'}
      <p class="muted">{$tStore('common.loading')}</p>
    {/if}
  {:else if listing.length === 0}
    <p class="muted">{$tStore('files.empty')}</p>
  {:else}
    <div class="tablewrap">
      <table class="grid">
        <thead>
          <tr>
            <th class="kind"><span class="sr-only">{$tStore('files.col.type')}</span></th>
            <th>{$tStore('files.col.name')}</th>
            <th class="num">{$tStore('files.col.size')}</th>
            <th class="hide-sm">{$tStore('files.col.mode')}</th>
            <th class="hide-sm">{$tStore('files.col.owner')}</th>
            <th>{$tStore('files.col.mtime')}</th>
            <th class="acts-head"><span class="sr-only">{$tStore('files.col.actions')}</span></th>
          </tr>
        </thead>
        <tbody>
          {#each listing as entry (entry.name)}
            <tr>
              <td class="kind">
                <span class="kind-icon kind-{entry.type}" role="img" title={kindLabel(entry.type)} aria-label={kindLabel(entry.type)}></span>
              </td>
              <td class="name">
                {#if entry.type === 'dir'}
                  <button class="link" type="button" onclick={() => go(joinPath(path, entry.name))}>{entry.name}/</button>
                {:else if entry.type === 'file' || entry.type === 'link'}
                  <button class="link" type="button" onclick={() => openFile(entry)}>{entry.name}</button>
                  {#if entry.type === 'link' && entry.link_target}
                    <span class="muted small"> → {entry.link_target}</span>
                  {/if}
                {:else}
                  <span>{entry.name}</span>
                {/if}
              </td>
              <td class="num mono">{entry.type === 'dir' ? '—' : formatBytes(entry.size)}</td>
              <td class="hide-sm mono small">{entry.mode_str}</td>
              <td class="hide-sm mono small">{entry.owner}:{entry.group}</td>
              <td class="mono small">{formatDateTime(entry.mtime, $locale)}</td>
              <td class="acts">
                <div class="row end">
                  {#if entry.type === 'file' || entry.type === 'link'}
                    <a
                      class="btn"
                      class:off={offline}
                      href={api.fileDownloadUrl(serverId, joinPath(path, entry.name))}
                      download={entry.name}
                      aria-disabled={offline}
                      tabindex={offline ? -1 : undefined}>{$tStore('files.download')}</a
                    >
                  {/if}
                  <button class="btn" type="button" disabled={readOnly} onclick={() => requestDelete(entry)}>
                    {$tStore('files.delete')}
                  </button>
                </div>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</div>

{#if prompt}
  {@const p = prompt}
  <Modal
    title={$tStore(p.kind === 'dir' ? 'files.promptDir' : 'files.promptFile')}
    labelId="files-prompt-title"
    onclose={() => {
      if (!p.busy) prompt = null;
    }}
  >
    <form class="stack" onsubmit={submitPrompt}>
      <label class="field">
        <span class="label">{$tStore('files.nameLabel')}</span>
        <input class="input" type="text" autocomplete="off" spellcheck="false" bind:value={p.name} disabled={p.busy} />
      </label>
      <p class="hint mono">{$tStore('files.inDir', { path })}</p>
      {#if p.error}
        <p class="error" role="alert">{p.error}</p>
      {/if}
      <div class="row end">
        <button class="btn" type="button" disabled={p.busy} onclick={() => (prompt = null)}>{$tStore('common.cancel')}</button>
        <button class="btn primary" type="submit" disabled={p.busy}>{$tStore('files.create')}</button>
      </div>
    </form>
  </Modal>
{/if}

{#if ask}
  {@const a = ask}
  <Modal
    title={a.title}
    labelId="files-ask-title"
    onclose={() => {
      if (!askBusy) ask = null;
    }}
  >
    <p>{a.text}</p>
    <div class="row end">
      <button class="btn" type="button" disabled={askBusy} onclick={() => (ask = null)}>{$tStore('common.cancel')}</button>
      <button class="btn danger" type="button" disabled={askBusy} onclick={confirmAsk}>{a.label}</button>
    </div>
  </Modal>
{/if}

{#if editor}
  {@const ed = editor}
  <div class="wide">
    <Modal title={ed.name} labelId="files-editor-title" onclose={closeEditor}>
      <p class="hint mono">{ed.path}</p>
      <textarea
        class="editor mono"
        bind:value={ed.text}
        spellcheck="false"
        autocapitalize="off"
        aria-label={ed.name}
        readonly={offline}
      ></textarea>

      {#if ed.conflict}
        <div class="strip" role="alert">
          <p>{$tStore('files.err.conflict')}</p>
          <div class="row end">
            <button class="btn" type="button" onclick={() => (ed.conflict = false)}>{$tStore('common.cancel')}</button>
            <button class="btn primary" type="button" onclick={reloadEditor}>{$tStore('files.editor.reload')}</button>
          </div>
        </div>
      {:else if ed.askClose}
        <div class="strip" role="alert">
          <p>{$tStore('files.editor.unsaved')}</p>
          <div class="row end">
            <button class="btn" type="button" onclick={() => (ed.askClose = false)}>{$tStore('files.editor.stay')}</button>
            <button class="btn danger" type="button" onclick={() => (editor = null)}>{$tStore('files.editor.discard')}</button>
          </div>
        </div>
      {/if}

      <div class="row between">
        <span class="small muted">{dirty ? $tStore('files.editor.dirty') : $tStore('files.editor.clean')}</span>
        <div class="row">
          <button class="btn" type="button" onclick={closeEditor}>{$tStore('files.editor.close')}</button>
          <button class="btn primary" type="button" disabled={!dirty || offline || ed.saving} onclick={saveEditor}>
            {ed.saving ? $tStore('files.editor.saving') : $tStore('files.editor.save')}
          </button>
        </div>
      </div>
    </Modal>
  </div>
{/if}

<style>
  .files {
    display: flex;
    flex-direction: column;
    gap: 12px;
    min-width: 0;
  }

  .toolbar {
    display: flex;
    flex-wrap: wrap;
    justify-content: space-between;
    align-items: center;
    gap: 8px;
  }

  .crumbs {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 2px;
    min-width: 0;
  }

  .crumb {
    padding: 2px 4px;
    background: none;
    border: 0;
    color: var(--accent);
    cursor: pointer;
    overflow-wrap: anywhere;
  }

  .crumb[aria-current='page'] {
    color: var(--ink);
    cursor: default;
  }

  .sep {
    color: var(--muted);
  }

  .actions {
    gap: 6px;
  }

  .picker {
    position: absolute;
    width: 1px;
    height: 1px;
    opacity: 0;
    pointer-events: none;
  }

  .notice {
    margin: 0;
    padding: 8px 12px;
    border: 2px solid var(--warn);
    color: var(--warn);
  }

  .upload {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .upload p {
    margin: 0;
  }

  .bar {
    height: 12px;
    border: 2px solid var(--line);
    background: var(--seg-off);
  }

  .bar > span {
    display: block;
    height: 100%;
    background: var(--accent);
  }

  .state {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 12px;
  }

  .tablewrap {
    overflow-x: auto;
  }

  table.grid {
    width: 100%;
    min-width: 520px;
    border-collapse: collapse;
  }

  th {
    padding: 8px;
    border-bottom: 2px solid var(--line);
    color: var(--muted);
    font-size: 12px;
    font-weight: 400;
    letter-spacing: 0.04em;
    text-align: left;
    text-transform: uppercase;
    white-space: nowrap;
  }

  td {
    padding: 8px;
    border-bottom: 2px dashed var(--seg-off);
    vertical-align: middle;
  }

  .num {
    text-align: right;
    white-space: nowrap;
  }

  .kind {
    width: 24px;
    padding-right: 0;
  }

  .kind-icon {
    display: inline-block;
    width: 12px;
    height: 12px;
    border: 2px solid var(--line);
  }

  .kind-dir {
    background: var(--accent);
    border-color: var(--accent);
  }

  .kind-file {
    background: none;
  }

  .kind-link {
    border-style: dashed;
  }

  .kind-other {
    background: var(--muted);
    border-color: var(--muted);
  }

  .name {
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .link {
    padding: 0;
    background: none;
    border: 0;
    color: var(--ink);
    cursor: pointer;
    font: inherit;
    text-align: left;
    overflow-wrap: anywhere;
  }

  .link:hover {
    color: var(--accent);
    text-decoration: underline;
  }

  .acts {
    width: 1%;
    white-space: nowrap;
  }

  .acts-head {
    width: 1%;
  }

  a.btn {
    display: inline-block;
    text-decoration: none;
  }

  .btn.off {
    opacity: 0.5;
    pointer-events: none;
    box-shadow: none;
  }

  .wide :global(.dialog) {
    max-width: 960px;
  }

  .editor {
    width: 100%;
    min-height: 50vh;
    padding: 8px 10px;
    background: var(--surface);
    color: var(--ink);
    border: 2px solid var(--line);
    border-radius: 0;
    font: inherit;
    font-size: 13px;
    line-height: 1.45;
    white-space: pre;
    overflow: auto;
    tab-size: 4;
    resize: vertical;
  }

  .editor:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }

  .strip {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 10px 12px;
    border: 2px solid var(--warn);
  }

  .strip p {
    margin: 0;
  }

  .between {
    justify-content: space-between;
  }

  .sr-only {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip: rect(0 0 0 0);
    white-space: nowrap;
  }

  @media (max-width: 720px) {
    .hide-sm {
      display: none;
    }

    table.grid {
      min-width: 0;
    }
  }
</style>
