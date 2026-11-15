<script lang="ts">
  import { get } from 'svelte/store';
  import { untrack } from 'svelte';
  import { api, type FileConflictMode, type FileEntry, type ServerDetail } from '../../../api';
  import { locale, t, tr as tStore, type Vars } from '../../../i18n';
  import Modal from '../../Modal.svelte';
  import { closeEditorWindows, openEditorWindow } from '../../terminal/windows';
  import ContextMenu, { type MenuItem } from '../../ui/ContextMenu.svelte';
  import { clip } from './files-clip';
  import { showToast } from '../../toast';
  import { formatBytes, formatDateTime } from '../format';
  import FilePropsModal from './FilePropsModal.svelte';
  import FileTree from './FileTree.svelte';
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

  let prompt = $state<{ kind: 'dir' | 'file' | 'rename'; name: string; error: string; busy: boolean; target?: FileEntry } | null>(null);
  let ask = $state<Ask | null>(null);
  let askBusy = $state(false);
  let upload = $state<{ name: string; index: number; total: number; fraction: number } | null>(null);
  let picker = $state<HTMLInputElement | undefined>(undefined);
  /** Selected names (click, Ctrl/⌘+click, Shift+click) and the row a Shift range starts from. */
  let sel = $state<string[]>([]);
  let anchor = $state<string | null>(null);
  let menu = $state<{ x: number; y: number; items: MenuItem[] } | null>(null);
  let pasting = $state(false);
  /** Full path of the entry whose properties are shown. */
  let propsPath = $state<string | null>(null);
  let conflict = $state<{ name: string; remaining: number; all: boolean; resolve: (r: { mode: FileConflictMode | null; all: boolean }) => void } | null>(null);
  let noEdit = $state<{ name: string; href: string; text: string } | null>(null);
  let treeOpen = $state(false);

  const TREE_KEY = 'linager.files.treeWidth';
  function savedTreeWidth(): number {
    try {
      const n = Number(localStorage.getItem(TREE_KEY));
      if (n >= 160 && n <= 600) return n;
    } catch {
      // storage unavailable
    }
    return 260;
  }
  let treeWidth = $state(savedTreeWidth());

  // The two panes fill the window below them and scroll inside, so the tab itself never grows.
  let panesEl = $state<HTMLDivElement | null>(null);
  let panesH = $state(480);
  function fitPanes(): void {
    if (!panesEl) return;
    const top = panesEl.getBoundingClientRect().top + window.scrollY;
    panesH = Math.max(320, Math.floor(window.innerHeight - top - 44));
  }
  $effect(() => {
    // Re-measure when something above the panes appears or disappears.
    void [panesEl, upload !== null, offline, loadState];
    fitPanes();
    window.addEventListener('resize', fitPanes);
    return () => window.removeEventListener('resize', fitPanes);
  });

  const offline = $derived(loadState === 'offline' || !server.online);
  const readOnly = $derived(offline || loadState === 'disabled');

  function T(key: string, vars?: Vars): string {
    return t(key, get(locale), vars);
  }

  function apply(res: { path: string; entries: FileEntry[] }): void {
    if (res.path !== path) {
      sel = [];
      anchor = null;
    }
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
        // the API takes the full target path (directory + file name), not just the directory
        await api.uploadFile(id, joinPath(dir, file.name), file, (fraction) => {
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
      if (kind === 'rename' && current.target) {
        if (name !== current.target.name) {
          const old = joinPath(path, current.target.name);
          await api.fileRename(id, old, name);
          closeEditorWindows(id, old);
          sel = [name];
          anchor = name;
        }
        prompt = null;
        showToast(T('files.renamed', { name }));
      } else {
        if (kind === 'dir') await api.fileMkdir(id, full);
        else await api.fileCreate(id, full);
        prompt = null;
        showToast(T('files.created', { name }));
      }
      await go(path, id);
    } catch (e) {
      current.busy = false;
      current.error = errorText(e, T);
    }
  }

  function requestDelete(entries: FileEntry[]): void {
    if (entries.length === 0) return;
    const id = serverId;
    const dir = path;
    const one = entries.length === 1 ? entries[0] : undefined;
    ask = {
      title: T(one ? 'files.deleteTitle' : 'files.deleteManyTitle'),
      text: one
        ? T(one.type === 'dir' ? 'files.deleteDir' : 'files.deleteFile', { path: joinPath(dir, one.name) })
        : T('files.deleteMany', { n: entries.length }),
      label: T('files.delete'),
      run: async () => {
        let done = 0;
        const failed: string[] = [];
        for (const entry of entries) {
          const full = joinPath(dir, entry.name);
          try {
            await api.fileDelete(id, full);
            closeEditorWindows(id, full);
            done++;
          } catch (e) {
            failed.push(`${entry.name}: ${errorText(e, T)}`);
          }
        }
        if (done > 0) showToast(one ? T('files.deleted', { name: one.name }) : T('files.deletedMany', { n: done }));
        if (failed.length > 0) showToast(failed.slice(0, 3).join('; '));
        sel = [];
        await go(path, id);
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
      openEditorWindow(serverId, server.name, full, entry.name, f.content, f.mtime);
    } catch (e) {
      if (isStatus(e, 413) || isStatus(e, 415)) {
        noEdit = { name: entry.name, href: api.fileDownloadUrl(serverId, full), text: openErrorText(e, T) };
      } else {
        showToast(openErrorText(e, T));
      }
    }
  }

  /** Double click / Enter: a folder opens in the list, a file opens in the editor. */
  function openEntry(entry: FileEntry): void {
    if (entry.type === 'dir') void go(joinPath(path, entry.name));
    else if (entry.type === 'file' || entry.type === 'link') void openFile(entry);
  }

  const coarse = () => window.matchMedia('(pointer: coarse)').matches;
  const onControl = (event: Event) => !!(event.target as HTMLElement).closest('a, button');

  const selectedEntries = (): FileEntry[] => (listing ?? []).filter((e) => sel.includes(e.name));
  const clipHere = $derived($clip && $clip.serverId === serverId ? $clip : null);

  function selectRange(to: FileEntry, add: boolean): void {
    const names = (listing ?? []).map((e) => e.name);
    const a = anchor ? names.indexOf(anchor) : -1;
    const b = names.indexOf(to.name);
    if (a < 0 || b < 0) {
      sel = [to.name];
      anchor = to.name;
      return;
    }
    const range = names.slice(Math.min(a, b), Math.max(a, b) + 1);
    sel = add ? [...new Set([...sel, ...range])] : range;
  }

  /** Click selects; Ctrl/⌘ toggles one row, Shift selects a range; on touch screens a tap opens right away. */
  function rowClick(event: MouseEvent, entry: FileEntry): void {
    if (onControl(event)) return;
    if (event.shiftKey) selectRange(entry, event.ctrlKey || event.metaKey);
    else if (event.ctrlKey || event.metaKey) {
      sel = sel.includes(entry.name) ? sel.filter((n) => n !== entry.name) : [...sel, entry.name];
      anchor = entry.name;
    } else {
      sel = [entry.name];
      anchor = entry.name;
    }
    if (coarse() && !event.shiftKey && !event.ctrlKey && !event.metaKey) openEntry(entry);
  }

  function rowKey(event: KeyboardEvent, entry: FileEntry, index: number): void {
    if (event.target !== event.currentTarget) return;
    const mod = event.ctrlKey || event.metaKey;
    const key = event.key.toLowerCase();
    if (event.key === 'Enter') {
      event.preventDefault();
      openEntry(entry);
    } else if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault();
      const rows = (event.currentTarget as HTMLElement).parentElement?.children;
      const next = rows?.[index + (event.key === 'ArrowDown' ? 1 : -1)] as HTMLElement | undefined;
      const target = listing?.[index + (event.key === 'ArrowDown' ? 1 : -1)];
      if (next && target) {
        if (event.shiftKey) selectRange(target, false);
        else {
          sel = [target.name];
          anchor = target.name;
        }
        next.focus();
      }
    } else if (mod && key === 'a') {
      event.preventDefault();
      sel = (listing ?? []).map((e) => e.name);
    } else if (mod && (key === 'c' || key === 'x') && !readOnly) {
      event.preventDefault();
      if (!sel.includes(entry.name)) sel = [entry.name];
      setClip(key === 'x' ? 'cut' : 'copy');
    } else if (mod && key === 'v' && !readOnly) {
      event.preventDefault();
      void paste(path);
    } else if (event.key === 'Delete' && !readOnly) {
      event.preventDefault();
      if (!sel.includes(entry.name)) sel = [entry.name];
      requestDelete(selectedEntries());
    } else if (event.key === 'F2' && !readOnly) {
      event.preventDefault();
      requestRename(entry);
    } else if (event.key === 'Escape') {
      sel = [];
    }
  }

  // ---- context menu ----

  function menuItems(entries: FileEntry[]): MenuItem[] {
    const lock = readOnly;
    const one = entries.length === 1 ? entries[0] : undefined;
    const items: MenuItem[] = [];
    if (one) {
      items.push({ key: 'open', label: T('files.menu.open'), onselect: () => openEntry(one) });
      if (one.type === 'dir') items.push({ key: 'zip', label: T('files.menu.downloadZip'), disabled: offline, onselect: () => downloadZip(entries) });
      else items.push({ key: 'dl', label: T('files.menu.download'), disabled: offline, onselect: () => downloadFile(one) });
    } else if (entries.length > 1) {
      items.push({ key: 'zip', label: T('files.menu.downloadZip'), disabled: offline, onselect: () => downloadZip(entries) });
    }
    if (entries.length > 0) {
      items.push({ key: 'cut', label: T('files.menu.cut'), disabled: lock, separated: true, onselect: () => setClip('cut') });
      items.push({ key: 'copy', label: T('files.menu.copy'), disabled: lock, onselect: () => setClip('copy') });
    }
    if (clipHere) {
      items.push({ key: 'paste', label: T('files.menu.paste'), disabled: lock || pasting, separated: entries.length === 0, onselect: () => void paste(path) });
      if (one?.type === 'dir') {
        items.push({ key: 'pasteInto', label: T('files.menu.pasteInto'), disabled: lock || pasting, onselect: () => void paste(joinPath(path, one.name)) });
      }
    }
    if (one) items.push({ key: 'rename', label: T('files.menu.rename'), disabled: lock, separated: true, onselect: () => requestRename(one) });
    if (one) items.push({ key: 'props', label: T('files.menu.props'), onselect: () => (propsPath = joinPath(path, one.name)) });
    if (entries.length > 0) {
      items.push({
        key: 'delete',
        label: entries.length > 1 ? T('files.menu.deleteN', { n: entries.length }) : T('files.menu.delete'),
        disabled: lock,
        danger: true,
        separated: !one,
        onselect: () => requestDelete(entries),
      });
    }
    if (entries.length === 0) {
      items.push({ key: 'mkdir', label: T('files.newFolder'), disabled: lock, separated: true, onselect: () => (prompt = { kind: 'dir', name: '', error: '', busy: false }) });
      items.push({ key: 'mkfile', label: T('files.newFile'), disabled: lock, onselect: () => (prompt = { kind: 'file', name: '', error: '', busy: false }) });
      items.push({ key: 'refresh', label: T('files.refresh'), onselect: () => void go(path) });
      items.push({ key: 'props', label: T('files.menu.props'), separated: true, onselect: () => (propsPath = path) });
    }
    return items;
  }

  function showMenu(x: number, y: number, entries: FileEntry[]): void {
    const items = menuItems(entries);
    menu = items.length > 0 ? { x, y, items } : null;
  }

  /** Right click on a row: the row joins the selection first unless it is already part of it. */
  function rowContext(event: MouseEvent, entry: FileEntry): void {
    event.preventDefault();
    event.stopPropagation();
    if (!sel.includes(entry.name)) {
      sel = [entry.name];
      anchor = entry.name;
    }
    showMenu(event.clientX, event.clientY, selectedEntries());
  }

  /** Right click on the empty part of the list. */
  function listContext(event: MouseEvent): void {
    event.preventDefault();
    sel = [];
    showMenu(event.clientX, event.clientY, []);
  }

  function moreClick(event: MouseEvent, entry: FileEntry): void {
    event.stopPropagation();
    if (!sel.includes(entry.name)) {
      sel = [entry.name];
      anchor = entry.name;
    }
    const r = (event.currentTarget as HTMLElement).getBoundingClientRect();
    showMenu(r.left, r.bottom, selectedEntries());
  }

  // ---- clipboard, rename, download ----

  function setClip(mode: 'cut' | 'copy'): void {
    const names = selectedEntries().map((e) => e.name);
    if (names.length === 0) return;
    clip.set({ mode, serverId, dir: path, names });
    showToast(T(mode === 'cut' ? 'files.cut' : 'files.copied', { n: names.length }));
  }

  function askConflict(name: string, remaining: number): Promise<{ mode: FileConflictMode | null; all: boolean }> {
    return new Promise((resolve) => {
      conflict = { name, remaining, all: false, resolve };
    });
  }

  function answerConflict(mode: FileConflictMode | null): void {
    const c = conflict;
    if (!c) return;
    conflict = null;
    c.resolve({ mode, all: c.all && c.remaining > 1 });
  }

  /** Pastes the clipboard into a folder. A name that already exists asks: overwrite or keep both (optionally for all). */
  async function paste(targetDir: string): Promise<void> {
    const c = $clip;
    if (!c || c.serverId !== serverId || pasting || readOnly) return;
    const id = serverId;
    pasting = true;
    try {
      let existing: Set<string>;
      if (targetDir === path && listing) existing = new Set(listing.map((e) => e.name));
      else existing = new Set((await api.files(id, targetDir)).entries.map((e) => e.name));
      const sameDir = c.dir === targetDir;
      if (c.mode === 'cut' && sameDir) {
        clip.set(null);
        return;
      }
      let remaining = c.names.filter((n) => existing.has(n) && !(c.mode === 'copy' && sameDir)).length;
      let forAll: FileConflictMode | null = null;
      let done = 0;
      const failed: string[] = [];
      let cancelled = false;
      for (const name of c.names) {
        let mode: FileConflictMode = 'fail';
        if (existing.has(name)) {
          if (c.mode === 'copy' && sameDir) mode = 'keep'; // copy next to the original: a free name
          else {
            if (forAll) mode = forAll;
            else {
              const r = await askConflict(name, remaining);
              if (!r.mode) {
                cancelled = true;
                break;
              }
              mode = r.mode;
              if (r.all) forAll = r.mode;
            }
            remaining--;
          }
        }
        const src = joinPath(c.dir, name);
        try {
          if (c.mode === 'cut') {
            await api.fileMove(id, src, targetDir, mode);
            closeEditorWindows(id, src);
          } else {
            await api.fileCopy(id, src, targetDir, mode);
          }
          done++;
        } catch (e) {
          failed.push(`${name}: ${errorText(e, T)}`);
        }
      }
      if (c.mode === 'cut' && !cancelled && failed.length === 0) clip.set(null);
      if (done > 0) showToast(T('files.paste.done', { n: done }));
      if (failed.length > 0) showToast(T('files.paste.failed', { list: failed.slice(0, 3).join('; ') }));
      await go(path, id);
    } catch (e) {
      showToast(errorText(e, T));
    } finally {
      pasting = false;
    }
  }

  function requestRename(entry: FileEntry): void {
    prompt = { kind: 'rename', name: entry.name, error: '', busy: false, target: entry };
  }

  function startDownload(url: string, name: string): void {
    const a = document.createElement('a');
    a.href = url;
    a.download = name;
    document.body.appendChild(a);
    a.click();
    a.remove();
  }

  function downloadFile(entry: FileEntry): void {
    startDownload(api.fileDownloadUrl(serverId, joinPath(path, entry.name)), entry.name);
  }

  function downloadZip(entries: FileEntry[]): void {
    const names = entries.map((e) => e.name);
    startDownload(api.fileZipUrl(serverId, path, names), names.length === 1 ? `${names[0]}.zip` : 'files.zip');
  }

  /** Drag of the splitter between the tree and the list. */
  function startSplit(event: PointerEvent): void {
    if (event.button !== 0) return;
    event.preventDefault();
    const handle = event.currentTarget as HTMLElement;
    handle.setPointerCapture(event.pointerId);
    const startX = event.clientX;
    const startW = treeWidth;
    const move = (e: PointerEvent) => {
      treeWidth = Math.min(Math.max(startW + e.clientX - startX, 160), 600);
    };
    const end = () => {
      handle.removeEventListener('pointermove', move);
      handle.removeEventListener('pointerup', end);
      handle.removeEventListener('pointercancel', end);
      try {
        localStorage.setItem(TREE_KEY, String(treeWidth));
      } catch {
        // storage unavailable
      }
    };
    handle.addEventListener('pointermove', move);
    handle.addEventListener('pointerup', end);
    handle.addEventListener('pointercancel', end);
  }

  function navigateTree(dir: string): void {
    treeOpen = false;
    void go(dir);
  }

  function kindLabel(type: FileEntry['type']): string {
    return T(`files.kind.${type}`);
  }
</script>

<div class="files">
  <div class="toolbar">
    <div class="where">
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
      {#if sel.length > 1}
        <span class="selcount small muted" role="status">{$tStore('files.selected', { n: sel.length })}</span>
      {/if}
    </div>

    <div class="row actions">
      <button class="btn tree-toggle" type="button" aria-expanded={treeOpen} onclick={() => (treeOpen = !treeOpen)}>
        {$tStore('files.tree')}
      </button>
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

  <div class="panes" class:tree-open={treeOpen} style:--tree-w="{treeWidth}px" style:--panes-h="{panesH}px" bind:this={panesEl}>
  {#if loadState !== 'offline' && loadState !== 'disabled'}
    <aside class="treepane">
      <FileTree {serverId} {path} entries={listing} onnavigate={navigateTree} />
    </aside>
    <div class="split" role="separator" aria-orientation="vertical" aria-label={$tStore('files.splitter')} onpointerdown={startSplit}></div>
  {/if}
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="listpane" oncontextmenu={listContext}>
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
            <th class="hide-sm">{$tStore('files.col.mode')}</th>
            <th class="hide-sm">{$tStore('files.col.owner')}</th>
            <th class="num">{$tStore('files.col.size')}</th>
            <th class="hide-xs">{$tStore('files.col.mtime')}</th>
            <th class="acts-head"><span class="sr-only">{$tStore('files.col.actions')}</span></th>
          </tr>
        </thead>
        <tbody>
          {#each listing as entry, i (entry.name)}
            <tr
              class="item"
              class:sel={sel.includes(entry.name)}
              class:cut={clipHere?.mode === 'cut' && clipHere.dir === path && clipHere.names.includes(entry.name)}
              tabindex="0"
              aria-selected={sel.includes(entry.name)}
              onclick={(e) => rowClick(e, entry)}
              ondblclick={(e) => !onControl(e) && openEntry(entry)}
              onkeydown={(e) => rowKey(e, entry, i)}
              oncontextmenu={(e) => rowContext(e, entry)}
            >
              <td class="kind">
                <span class="kind-icon kind-{entry.type}" role="img" title={kindLabel(entry.type)} aria-label={kindLabel(entry.type)}></span>
              </td>
              <td class="name">
                {#if entry.type === 'dir'}
                  <span>{entry.name}/</span>
                {:else}
                  <span>{entry.name}</span>
                  {#if entry.type === 'link' && entry.link_target}
                    <span class="muted small"> → {entry.link_target}</span>
                  {/if}
                {/if}
              </td>
              <td class="hide-sm mono small">{entry.mode_str}</td>
              <td class="hide-sm mono small">{entry.owner}:{entry.group}</td>
              <td class="num mono">{entry.type === 'dir' ? '—' : formatBytes(entry.size)}</td>
              <td class="hide-xs mono small">{formatDateTime(entry.mtime, $locale)}</td>
              <td class="acts">
                <button class="btn more" type="button" aria-label={$tStore('files.menu.more')} onclick={(e) => moreClick(e, entry)}>⋯</button>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
  </div>
  </div>
</div>

{#if prompt}
  {@const p = prompt}
  <Modal
    title={$tStore(p.kind === 'dir' ? 'files.promptDir' : p.kind === 'rename' ? 'files.promptRename' : 'files.promptFile')}
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
        <button class="btn primary" type="submit" disabled={p.busy}>{$tStore(p.kind === 'rename' ? 'files.rename.submit' : 'files.create')}</button>
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

{#if noEdit}
  {@const n = noEdit}
  <Modal title={n.name} labelId="files-noedit-title" onclose={() => (noEdit = null)}>
    <p>{n.text}</p>
    <div class="row end">
      <button class="btn" type="button" onclick={() => (noEdit = null)}>{$tStore('common.cancel')}</button>
      <a class="btn primary" href={n.href} download={n.name} onclick={() => (noEdit = null)}>{$tStore('files.download')}</a>
    </div>
  </Modal>
{/if}

{#if propsPath}
  <FilePropsModal
    {serverId}
    path={propsPath}
    {offline}
    onclose={() => (propsPath = null)}
    onchanged={() => void go(path)}
    onauthlost={() => (propsPath = null)} />
{/if}

{#if menu}
  <ContextMenu x={menu.x} y={menu.y} items={menu.items} label={$tStore('files.menu')} onclose={() => (menu = null)} />
{/if}

{#if conflict}
  {@const c = conflict}
  <Modal title={$tStore('files.conflict.title')} labelId="files-conflict-title" onclose={() => answerConflict(null)}>
    <div class="stack">
      <p>{$tStore('files.conflict.text', { name: c.name })}</p>
      {#if c.remaining > 1}
        <label class="check">
          <input type="checkbox" bind:checked={c.all} />
          <span>{$tStore('files.conflict.all', { n: c.remaining })}</span>
        </label>
      {/if}
      <div class="row end">
        <button class="btn" type="button" onclick={() => answerConflict(null)}>{$tStore('files.conflict.cancel')}</button>
        <button class="btn" type="button" onclick={() => answerConflict('keep')}>{$tStore('files.conflict.keep')}</button>
        <button class="btn danger" type="button" onclick={() => answerConflict('overwrite')}>{$tStore('files.conflict.overwrite')}</button>
      </div>
    </div>
  </Modal>
{/if}

<style>
  tr.cut {
    opacity: 0.5;
  }

  /* The "more" button replaces the old row buttons on touch screens; with a mouse the context menu is used. */
  .more {
    display: none;
    padding: 2px 10px;
  }

  @media (pointer: coarse) {
    .more {
      display: inline-block;
    }
  }

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

  /* The path with the number of selected items to its right. */
  .where {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 4px 12px;
    min-width: 0;
  }

  .selcount {
    white-space: nowrap;
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

  .panes {
    display: grid;
    grid-template-columns: var(--tree-w) 8px minmax(0, 1fr);
    align-items: stretch;
    height: var(--panes-h);
  }

  .treepane {
    min-width: 0;
    min-height: 0;
    overflow: auto;
    padding: 6px 4px;
    border: 2px solid var(--line);
    background: var(--surface);
  }

  .split {
    cursor: col-resize;
    touch-action: none;
  }

  .split:hover {
    background: var(--seg-off);
  }

  .listpane {
    min-width: 0;
    min-height: 0;
    overflow: auto;
    container-type: inline-size;
  }

  /* The list pane scrolls both ways, so the header row can stick to its top. */
  .listpane .tablewrap {
    overflow: visible;
  }

  .listpane th {
    position: sticky;
    top: 0;
    z-index: 1;
    background: var(--surface);
  }

  /* Without the tree (offline, module disabled) the list takes the whole width. */
  .panes:not(:has(.treepane)) {
    grid-template-columns: minmax(0, 1fr);
  }

  .tree-toggle {
    display: none;
  }

  tr.item {
    cursor: default;
    user-select: none;
  }

  tr.item:hover td {
    background: color-mix(in srgb, var(--seg-off) 50%, transparent);
  }

  tr.item.sel td {
    background: var(--seg-off);
  }

  tr.item:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
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
    word-break: normal;
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

  @container (max-width: 460px) {
    .hide-xs {
      display: none;
    }
  }

  @container (max-width: 640px) {
    .hide-sm {
      display: none;
    }

    table.grid {
      min-width: 0;
    }
  }

  /* Phones: the tree is a panel above the list, opened with the "Folders" button. */
  @media (max-width: 720px) {
    .tree-toggle {
      display: inline-block;
    }

    .panes,
    .panes:not(:has(.treepane)) {
      grid-template-columns: minmax(0, 1fr);
      gap: 8px;
      height: auto;
    }

    .listpane {
      overflow: visible;
    }

    .treepane,
    .split {
      display: none;
    }

    .panes.tree-open .treepane {
      display: block;
      max-height: 50vh;
    }
  }
</style>
