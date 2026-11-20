<script lang="ts">
  import { get } from 'svelte/store';
  import { untrack } from 'svelte';
  import { api, type FileConflictMode, type FileEntry, type ServerDetail } from '../../../api';
  import { locale, t, tr as tStore, type Vars } from '../../../i18n';
  import Modal from '../../Modal.svelte';
  import { closeEditorWindows, openEditorWindow } from '../../terminal/windows';
  import ContextMenu, { type MenuItem } from '../../ui/ContextMenu.svelte';
  import PixIcon from '../../ui/PixIcon.svelte';
  import { isPhone } from '../../ui/viewport';
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
    type SortDir,
    type SortKey,
    unavailable,
  } from './files-util';

  let { server, embedded = false }: { server: ServerDetail; embedded?: boolean } = $props();

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
  let menu = $state<{ x: number; y: number; items: MenuItem[]; label?: string } | null>(null);
  let pasting = $state(false);
  /** Full path of the entry whose properties are shown. */
  let propsPath = $state<string | null>(null);
  let conflict = $state<{ name: string; remaining: number; all: boolean; resolve: (r: { mode: FileConflictMode | null; all: boolean }) => void } | null>(null);
  let noEdit = $state<{ name: string; href: string; text: string } | null>(null);
  let treeOpen = $state(false);
  /** Phone layout: the select mode (a check box on every row), the name filter and the path line. */
  let selectMode = $state(false);
  let searching = $state(false);
  let filterText = $state('');
  let pathEl = $state<HTMLElement | null>(null);
  let pathClipped = $state(false);

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
    // In a floating window the panes fill the window by CSS; only the page layout needs measuring.
    if (embedded || !panesEl) return;
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

  /** The list order: the column and the direction (the name, ascending, by default; folders stay on top). */
  let sortKey = $state<SortKey>('name');
  let sortDir = $state<SortDir>('asc');
  function sortBy(key: SortKey): void {
    if (sortKey === key) sortDir = sortDir === 'asc' ? 'desc' : 'asc';
    else {
      sortKey = key;
      // size and date are most useful with the biggest and newest first
      sortDir = key === 'size' || key === 'mtime' ? 'desc' : 'asc';
    }
    if (listing) listing = sortEntries(listing, sortKey, sortDir);
  }
  function ariaSort(key: SortKey): 'ascending' | 'descending' | 'none' {
    return sortKey !== key ? 'none' : sortDir === 'asc' ? 'ascending' : 'descending';
  }

  const offline = $derived(loadState === 'offline' || !server.online);
  const readOnly = $derived(offline || loadState === 'disabled');

  function T(key: string, vars?: Vars): string {
    return t(key, get(locale), vars);
  }

  function apply(res: { path: string; entries: FileEntry[] }): void {
    if (res.path !== path) {
      sel = [];
      anchor = null;
      filterText = '';
    }
    path = res.path;
    listing = sortEntries(res.entries, sortKey, sortDir);
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
        selectMode = false;
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

  // ---- phone layout ----

  /** The order of the action sheet of one entry: what is needed most comes first. */
  const SHEET_ORDER = ['open', 'dl', 'zip', 'props', 'rename', 'copy', 'cut', 'paste', 'pasteInto', 'delete'];

  /** A tap on a file (or the "more" button of a folder) opens its actions as a sheet: no right click or long press. */
  function openActions(entry: FileEntry): void {
    sel = [entry.name];
    anchor = entry.name;
    const items = menuItems([entry])
      .map((i) => (i.key === 'props' ? { ...i, label: T('files.m.rights') } : i))
      .sort((a, b) => SHEET_ORDER.indexOf(a.key) - SHEET_ORDER.indexOf(b.key))
      .map((i) => ({ ...i, separated: false }));
    menu = { x: 0, y: 0, items, label: entry.name };
  }

  function openCreate(): void {
    const lock = readOnly || !listing;
    menu = {
      x: 0,
      y: 0,
      label: T('files.m.create'),
      items: [
        { key: 'mkfile', label: T('files.newFile'), disabled: lock, onselect: () => (prompt = { kind: 'file', name: '', error: '', busy: false }) },
        { key: 'mkdir', label: T('files.newFolder'), disabled: lock, onselect: () => (prompt = { kind: 'dir', name: '', error: '', busy: false }) },
        { key: 'upload', label: T('files.upload'), disabled: lock || upload !== null, onselect: () => picker?.click() },
        { key: 'refresh', label: T('files.refresh'), disabled: !listing, onselect: () => void go(path) },
      ],
    };
  }

  function phoneTap(entry: FileEntry): void {
    if (selectMode) {
      sel = sel.includes(entry.name) ? sel.filter((n) => n !== entry.name) : [...sel, entry.name];
      return;
    }
    if (entry.type === 'dir') void go(joinPath(path, entry.name));
    else openActions(entry);
  }

  function toggleSelectMode(): void {
    selectMode = !selectMode;
    sel = [];
    anchor = null;
  }

  function copySelected(mode: 'cut' | 'copy'): void {
    setClip(mode);
    selectMode = false;
    sel = [];
  }

  const shown = $derived.by(() => {
    const q = filterText.trim().toLowerCase();
    return q ? (listing ?? []).filter((e) => e.name.toLowerCase().includes(q)) : listing;
  });

  /** "3 nov", with the year when it is not this one. */
  function shortDate(ts: number): string {
    const d = new Date(ts * 1000);
    if (Number.isNaN(d.getTime())) return '';
    const sameYear = d.getFullYear() === new Date().getFullYear();
    return d.toLocaleDateString($locale, sameYear ? { day: 'numeric', month: 'short' } : { day: 'numeric', month: 'short', year: 'numeric' }).replace(/\.(?=\s|$)/g, '');
  }

  function metaLine(entry: FileEntry): string {
    if (entry.type === 'link' && entry.link_target) return `→ ${entry.link_target}`;
    const parts = [entry.type === 'dir' ? kindLabel('dir').toLowerCase() : formatBytes(entry.size), shortDate(entry.mtime)];
    if (entry.type !== 'dir' && entry.owner) parts.push(entry.owner);
    return parts.filter(Boolean).join(' · ');
  }

  // The path line shows its end: the beginning is cut off (marked with "…") when the path is long.
  $effect(() => {
    void [path, $isPhone, listing];
    const el = pathEl;
    if (!el) return;
    el.scrollLeft = el.scrollWidth;
    pathClipped = el.scrollWidth > el.clientWidth + 1;
  });


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

{#snippet sortHead(key: SortKey, label: string)}
  <button class="sorter" type="button" onclick={() => sortBy(key)}>
    {$tStore(label)}<span class="arrow" aria-hidden="true">{sortKey === key ? (sortDir === 'asc' ? '▲' : '▼') : ''}</span>
  </button>
{/snippet}

<div class="files" class:embedded>
  <input class="picker" bind:this={picker} type="file" multiple tabindex="-1" aria-hidden="true" onchange={onPick} />
  {#if $isPhone}
    <div class="ptools">
      <button class="btn sm" type="button" aria-expanded={treeOpen} onclick={() => (treeOpen = true)}>
        <PixIcon name="folder" />
        {$tStore('files.tree')}
      </button>
      <button class="btn sm ico" type="button" aria-label={$tStore('files.m.search')} aria-pressed={searching} onclick={() => { searching = !searching; if (!searching) filterText = ''; }}>
        <PixIcon name="search" />
      </button>
      <button class="btn sm grow" type="button" aria-pressed={selectMode} disabled={!listing} onclick={toggleSelectMode}>
        {$tStore(selectMode ? 'files.m.done' : 'files.m.select')}
      </button>
      <button class="btn sm ico" type="button" aria-label={$tStore('files.m.create')} disabled={readOnly || !listing} onclick={openCreate}>
        <PixIcon name="plus" />
      </button>
    </div>
    {#if searching}
      <input class="input" type="search" placeholder={$tStore('files.m.search')} aria-label={$tStore('files.m.search')} autocomplete="off" bind:value={filterText} />
    {/if}
    <div class="pathrow">
      {#if pathClipped}<span class="muted" aria-hidden="true">…</span>{/if}
      <nav class="pathline" aria-label={$tStore('files.pathLabel')} bind:this={pathEl}>
        <button class="crumb" type="button" aria-current={path === '/' ? 'page' : undefined} onclick={() => go('/')}>/</button>
        {#each crumbs(path) as c, i (c.path)}
          {#if i > 0}<span class="sep" aria-hidden="true">/</span>{/if}
          <button class="crumb" type="button" aria-current={c.path === path ? 'page' : undefined} onclick={() => go(c.path)}>{c.name}</button>
        {/each}
      </nav>
    </div>
  {:else}
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
          <span class="sep" aria-hidden="true"><PixIcon name="next" size={12} /></span>
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
      </div>
    </div>

  {/if}

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

  {#if !$isPhone}
    <div class="panes" class:embedded style:--tree-w="{treeWidth}px" style:--panes-h="{panesH}px" bind:this={panesEl}>
    {#if loadState !== 'offline' && loadState !== 'disabled'}
      <aside class="treepane">
        <FileTree {serverId} {path} entries={listing} onnavigate={navigateTree} />
      </aside>
      <div class="split" role="separator" aria-orientation="vertical" aria-label={$tStore('files.splitter')} onpointerdown={startSplit}></div>
    {/if}
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="listpane" oncontextmenu={listContext}>
    {#if !listing || listing.length === 0}
      {@render placeholder()}
    {:else}
      <div class="tablewrap">
        <table class="grid">
          <thead>
            <tr>
              <th class="kind"><span class="sr-only">{$tStore('files.col.type')}</span></th>
              <th aria-sort={ariaSort('name')}>{@render sortHead('name', 'files.col.name')}</th>
              <th class="hide-sm" aria-sort={ariaSort('mode')}>{@render sortHead('mode', 'files.col.mode')}</th>
              <th class="hide-sm" aria-sort={ariaSort('owner')}>{@render sortHead('owner', 'files.col.owner')}</th>
              <th class="num" aria-sort={ariaSort('size')}>{@render sortHead('size', 'files.col.size')}</th>
              <th class="hide-xs" aria-sort={ariaSort('mtime')}>{@render sortHead('mtime', 'files.col.mtime')}</th>
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
  {:else}
    {#if !listing || (shown && shown.length === 0 && !filterText.trim())}
      {@render placeholder()}
    {:else}
      <div class="plist">
        {#if path !== '/' && !selectMode && !filterText.trim()}
          <button class="li" type="button" onclick={() => go(parentPath(path))}>
            <PixIcon name="back" />
            <span class="mn"><span class="l1"><b>..</b></span></span>
          </button>
        {/if}
        {#each shown ?? [] as entry (entry.name)}
          {@const picked = sel.includes(entry.name)}
          <div class="li" class:picked class:cut={clipHere?.mode === 'cut' && clipHere.dir === path && clipHere.names.includes(entry.name)}>
            <button
              class="main"
              type="button"
              role={selectMode ? 'checkbox' : undefined}
              aria-checked={selectMode ? picked : undefined}
              onclick={() => phoneTap(entry)}
            >
              {#if selectMode}<span class="box" class:on={picked} aria-hidden="true"></span>{/if}
              {#if entry.type === 'dir'}
                <PixIcon name="folder" />
              {:else}
                <span class="fileic"><PixIcon name="file" size={16} /></span>
              {/if}
              <span class="mn">
                <span class="l1"><b>{entry.name}</b></span>
                <span class="l2">{metaLine(entry)}</span>
              </span>
              {#if entry.type !== 'dir' && !selectMode}<PixIcon name="more" />{/if}
            </button>
            {#if entry.type === 'dir' && !selectMode}
              <button class="more-btn" type="button" aria-label={$tStore('files.menu.more')} onclick={() => openActions(entry)}>
                <PixIcon name="more" />
              </button>
            {/if}
          </div>
        {/each}
        {#if shown && shown.length === 0}
          <p class="muted empty">{$tStore('files.m.noMatch')}</p>
        {/if}
      </div>
    {/if}
  {/if}

  {#if $isPhone && selectMode}
    <div class="pbar" role="toolbar" aria-label={$tStore('files.menu')}>
      <div class="pbar-top">
        <span role="status">{$tStore('files.selected', { n: sel.length })}</span>
        <button class="btn sm" type="button" onclick={() => (sel = (shown ?? []).map((e) => e.name))}>{$tStore('files.m.all')}</button>
      </div>
      <div class="pbar-btns">
        <button class="btn sm" type="button" disabled={readOnly || sel.length === 0} onclick={() => copySelected('copy')}>{$tStore('files.menu.copy')}</button>
        <button class="btn sm" type="button" disabled={readOnly || sel.length === 0} onclick={() => copySelected('cut')}>{$tStore('files.menu.cut')}</button>
        <button class="btn sm" type="button" disabled={sel.length !== 1} onclick={() => (propsPath = joinPath(path, sel[0]))}>{$tStore('files.m.rightsShort')}</button>
        <button class="btn sm danger" type="button" disabled={readOnly || sel.length === 0} onclick={() => requestDelete(selectedEntries())}>{$tStore('files.menu.delete')}</button>
      </div>
    </div>
  {:else if $isPhone && clipHere}
    <div class="pbar" role="toolbar" aria-label={$tStore('files.menu')}>
      <div class="pbar-top">
        <span role="status">{$tStore('files.m.clip', { n: clipHere.names.length })}</span>
        <button class="btn sm" type="button" onclick={() => clip.set(null)}>{$tStore('files.m.clipClear')}</button>
      </div>
      <div class="pbar-btns">
        <button class="btn sm primary" type="button" disabled={readOnly || pasting} onclick={() => void paste(path)}>{$tStore('files.menu.paste')}</button>
      </div>
    </div>
  {/if}
</div>

{#snippet placeholder()}
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
  {:else}
    <p class="muted">{$tStore('files.empty')}</p>
  {/if}
{/snippet}

{#if $isPhone && treeOpen}
  <Modal title={$tStore('files.tree')} labelId="files-tree-title" flush onclose={() => (treeOpen = false)}>
    <FileTree {serverId} {path} entries={listing} onnavigate={navigateTree} />
  </Modal>
{/if}

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
  <ContextMenu x={menu.x} y={menu.y} items={menu.items} label={menu.label ?? $tStore('files.menu')} onclose={() => (menu = null)} />
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
    padding: 2px 9px;
  }

  @media (pointer: coarse) {
    .more {
      display: inline-block;
    }
  }

  .files {
    display: flex;
    flex-direction: column;
    gap: 11px;
    min-width: 0;
  }

  .toolbar {
    display: flex;
    flex-wrap: wrap;
    justify-content: space-between;
    align-items: center;
    gap: 7px;
  }

  /* The path with the number of selected items to its right. */
  .where {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 4px 11px;
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
    gap: 5px;
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
    padding: 7px 11px;
    border: 2px solid var(--warn);
    color: var(--warn);
  }

  .upload {
    display: flex;
    flex-direction: column;
    gap: 5px;
  }

  .upload p {
    margin: 0;
  }

  .bar {
    height: 11px;
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
    gap: 11px;
  }

  .panes {
    display: grid;
    grid-template-columns: var(--tree-w) 7px minmax(0, 1fr);
    align-items: stretch;
    height: var(--panes-h);
  }

  /* In a floating window the tab fills it and only the two panes scroll, never the window body. */
  .files.embedded {
    height: 100%;
    min-height: 0;
  }

  .panes.embedded {
    flex: 1 1 0;
    min-height: 0;
    height: auto;
  }

  .treepane {
    min-width: 0;
    min-height: 0;
    overflow: auto;
    padding: 5px 4px;
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
    min-width: 468px;
    border-collapse: collapse;
  }

  th {
    padding: 7px;
    border-bottom: 2px solid var(--line);
    color: var(--muted);
    font-size: 11px;
    font-weight: 400;
    letter-spacing: 0.04em;
    text-align: left;
    text-transform: uppercase;
    white-space: nowrap;
  }

  /* The column title is a button that sorts the list. */
  .sorter {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 0;
    border: 0;
    background: none;
    color: inherit;
    font: inherit;
    letter-spacing: inherit;
    text-transform: inherit;
    cursor: pointer;
  }

  .sorter:hover,
  th[aria-sort='ascending'] .sorter,
  th[aria-sort='descending'] .sorter {
    color: var(--ink);
  }

  .sorter:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }

  .arrow {
    display: inline-block;
    min-width: 8px;
    font-size: 8px;
  }

  td {
    padding: 7px;
    border-bottom: 2px dashed var(--seg-off);
    vertical-align: middle;
  }

  .num {
    text-align: right;
    white-space: nowrap;
  }

  .kind {
    width: 22px;
    padding-right: 0;
  }

  .kind-icon {
    display: inline-block;
    width: 11px;
    height: 11px;
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

  /* ---- Phones: one column, a list of rows, the folder tree and the actions as sheets ---- */
  .ptools {
    display: flex;
    align-items: center;
    gap: 7px;
  }

  .ptools .btn {
    gap: 5px;
    padding: 0 11px;
  }

  .ptools .ico {
    flex: none;
    width: 44px;
    padding: 0;
  }

  .ptools .grow {
    flex: 1;
    min-width: 0;
  }

  .ptools .btn[aria-pressed='true'] {
    background: var(--ink);
    color: var(--surface);
  }

  .pathrow {
    display: flex;
    align-items: center;
    gap: 4px;
    min-width: 0;
    font-size: 13px;
  }

  .pathline {
    display: flex;
    align-items: center;
    flex: 1;
    min-width: 0;
    overflow: hidden;
    white-space: nowrap;
  }

  .pathline .crumb {
    flex: none;
    min-height: 44px;
    padding: 0 5px;
    overflow-wrap: normal;
  }

  .pathline .sep {
    flex: none;
  }

  .plist {
    background: var(--surface);
    border: 2px solid var(--line);
    box-shadow: 3px 3px 0 var(--shade);
  }

  .plist .li {
    display: flex;
    align-items: center;
    width: 100%;
    min-height: 56px;
    padding: 0;
    background: none;
    border: 0;
    border-top: 2px dashed var(--seg-off);
    color: var(--ink);
    font: inherit;
    text-align: left;
  }

  .plist > :first-child {
    border-top: 0;
  }

  .plist button.li {
    gap: 9px;
    padding: 7px 7px 7px 11px;
    cursor: pointer;
  }

  .plist .li.picked {
    background: var(--seg-off);
  }

  .plist .li.cut {
    opacity: 0.5;
  }

  .plist .main {
    display: flex;
    align-items: center;
    gap: 9px;
    flex: 1;
    min-width: 0;
    min-height: 56px;
    padding: 7px 11px 7px 11px;
    background: none;
    border: 0;
    color: inherit;
    font: inherit;
    text-align: left;
    cursor: pointer;
  }

  .plist .main:has(+ .more-btn) {
    padding-right: 0;
  }

  .plist .more-btn {
    flex: none;
    display: flex;
    align-items: center;
    justify-content: center;
    width: 44px;
    height: 44px;
    margin-right: 0;
    padding: 0;
    background: none;
    border: 0;
    color: var(--ink);
    cursor: pointer;
  }

  .plist .mn {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
  }

  .plist .l1 {
    display: block;
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
    font-size: 13px;
  }

  .plist .l1 b {
    font-weight: 400;
  }

  .plist .l2 {
    display: block;
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
    color: var(--muted);
    font-size: 10px;
  }

  .fileic {
    flex: none;
    display: inline-flex;
  }

  .box {
    flex: none;
    width: 20px;
    height: 20px;
    border: 2px solid var(--line);
    background: var(--surface);
  }

  .box.on {
    background: var(--accent);
    box-shadow: inset 0 0 0 3px var(--surface);
  }

  .empty {
    padding: 13px 11px;
  }

  /* The actions of the selection (or the clipboard) stay above the bottom bar while the list scrolls. */
  .pbar {
    position: sticky;
    bottom: calc(var(--bar-h) + var(--safe-b));
    z-index: 5;
    display: flex;
    flex-direction: column;
    gap: 7px;
    margin: 0 -11px;
    padding: 7px 11px;
    background: var(--surface);
    border: 0;
    border-top: 2px solid var(--line);
    box-shadow: 0 -4px 0 var(--shade);
  }

  .pbar-top {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 7px;
    padding-left: 4px;
    font-size: 13px;
  }

  .pbar-btns {
    display: flex;
    gap: 5px;
  }

  .pbar-btns .btn {
    flex: 1;
    min-width: 0;
    padding: 0 4px;
    font-size: 12px;
  }
</style>
