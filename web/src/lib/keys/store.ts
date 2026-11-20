// The open vault of the page. The decrypted entries live here, in memory, and nowhere else: not in the local storage, not
// in the session storage, not on the server. A closed tab, a reload or the auto-lock forgets them; the file is the only copy.

import { get, writable } from 'svelte/store';
import { api, type Group, type Server } from '../../api';
import { DEFAULT_FILE, VaultError, cleanEntry, newSession, openVault, sealVault, wipe, type Entry, type Session } from './vault';

export interface VaultState {
  /** The name of the file it was opened from, and the name a save is offered under. */
  file: string;
  entries: Entry[];
  /** Entries were changed after the last save of the file. */
  dirty: boolean;
}

/** null while the vault is closed. */
export const vault = writable<VaultState | null>(null);

/** The window "Keys" is open. A form (the RDP form) opens it from where it is, so the state is here and not in the app. */
export const keysOpen = writable(false);

/** The auto-lock time was reached but there are unsaved changes: the vault stays open until the file is saved. */
export const lockWaiting = writable(false);

/** Minutes of silence before the vault closes itself; 0 turns it off. A harmless preference of this browser. */
export const LOCK_CHOICES = [5, 15, 30, 0] as const;
const LOCK_KEY = 'linager.keys.lock';

function savedLock(): number {
  try {
    const n = Number(localStorage.getItem(LOCK_KEY));
    return (LOCK_CHOICES as readonly number[]).includes(n) && localStorage.getItem(LOCK_KEY) !== null ? n : 15;
  } catch {
    return 15;
  }
}

export const lockMinutes = writable<number>(savedLock());

export function setLockMinutes(minutes: number): void {
  lockMinutes.set(minutes);
  try {
    localStorage.setItem(LOCK_KEY, String(minutes));
  } catch {
    // storage unavailable: the choice just lasts until the page is reloaded
  }
  touch();
}

let session: Session | null = null;
let timer: ReturnType<typeof setTimeout> | undefined;

function set(next: VaultState | null): void {
  vault.set(next);
  syncGuard(next?.dirty ?? false);
}

// ---- losing the changes by closing the tab ----

let guarded = false;
function onBeforeUnload(event: BeforeUnloadEvent): void {
  event.preventDefault();
  event.returnValue = '';
}
function syncGuard(dirty: boolean): void {
  if (dirty === guarded) return;
  guarded = dirty;
  if (dirty) window.addEventListener('beforeunload', onBeforeUnload);
  else window.removeEventListener('beforeunload', onBeforeUnload);
}

// ---- auto-lock ----

/** Call on every use of the vault (a click in the window, a key picked in a form): the time of silence starts again. */
export function touch(): void {
  clearTimeout(timer);
  if (!get(vault)) return;
  const minutes = get(lockMinutes);
  if (minutes <= 0) return;
  timer = setTimeout(() => {
    const state = get(vault);
    if (!state) return;
    if (state.dirty) lockWaiting.set(true);
    else closeVault();
  }, minutes * 60_000);
}

/** Forgets everything that was decrypted: the key and the entries. */
export function closeVault(): void {
  clearTimeout(timer);
  wipe(session);
  session = null;
  lockWaiting.set(false);
  set(null);
}

// ---- the file ----

function download(bytes: Uint8Array, name: string): void {
  const url = URL.createObjectURL(new Blob([bytes as BlobPart], { type: 'application/octet-stream' }));
  const a = document.createElement('a');
  a.href = url;
  a.download = name;
  document.body.appendChild(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 10_000);
}

/** A new empty vault with this passphrase; the file is offered for download at once, so that it exists before anything is put in it. */
export async function createVault(passphrase: string): Promise<void> {
  closeVault();
  session = await newSession(passphrase);
  download(sealVault([], session), DEFAULT_FILE);
  set({ file: DEFAULT_FILE, entries: [], dirty: false });
  touch();
}

/** Opens a file. Throws VaultError (`format`, `version`, `auth`, `limit`). */
export async function openFile(file: File, passphrase: string): Promise<void> {
  if (file.size > 1 << 20) throw new VaultError('limit');
  const bytes = new Uint8Array(await file.arrayBuffer());
  const opened = await openVault(bytes, passphrase);
  closeVault();
  session = opened.session;
  set({ file: file.name || DEFAULT_FILE, entries: opened.entries, dirty: false });
  touch();
}

/** Encrypts the entries again and offers the file for download. */
export function saveFile(): void {
  const state = get(vault);
  if (!state || !session) return;
  download(sealVault(state.entries, session), state.file);
  set({ ...state, dirty: false });
  if (get(lockWaiting)) closeVault();
  else touch();
}

// ---- the entries ----

/** Adds an entry or replaces the one with the same id. Returns false when the entry is not valid. */
export function putEntry(entry: Entry): boolean {
  const state = get(vault);
  const clean = cleanEntry(entry);
  if (!state || !clean) return false;
  const at = state.entries.findIndex((e) => e.id === clean.id);
  const entries = at < 0 ? [...state.entries, clean] : state.entries.map((e, i) => (i === at ? clean : e));
  set({ ...state, entries, dirty: true });
  touch();
  return true;
}

export function removeEntry(id: string): void {
  const state = get(vault);
  if (!state) return;
  set({ ...state, entries: state.entries.filter((e) => e.id !== id), dirty: true });
  touch();
}

// ---- the servers and the groups of the panel (what a key can be bound to) ----

export interface Directory {
  servers: Server[];
  groups: Group[];
}

let cached: { at: number; value: Directory } | null = null;

/** The servers this person may see and the groups of the panel. Kept for half a minute, so that a form and a window do not both ask. */
export async function loadDirectory(force = false): Promise<Directory> {
  if (!force && cached && Date.now() - cached.at < 30_000) return cached.value;
  const [servers, groups] = await Promise.all([api.servers(), api.groups()]);
  cached = { at: Date.now(), value: { servers, groups } };
  return cached.value;
}
