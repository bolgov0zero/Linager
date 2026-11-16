import { get, writable } from 'svelte/store';
import { t } from '../../i18n';
import { showToast } from '../toast';

/** Simultaneous terminals per server allowed by the server (server/api_mgmt.go termLimit). */
export const TERM_LIMIT = 3;

/** Floating windows share one store, z-order and dock: terminals and file editors. */
export interface TermWindow {
  key: number;
  kind: 'terminal' | 'editor';
  serverId: string;
  /** Terminal: server name. Editor: file name. */
  name: string;
  /** Editor only: file path, server name and the content loaded when the window was opened. */
  path?: string;
  serverName?: string;
  /** Terminal: the account the shell runs as (the Users tab); empty for root. */
  asUser?: string;
  initialText?: string;
  initialMtime?: number;
  x: number;
  y: number;
  w: number;
  h: number;
  z: number;
  minimized: boolean;
  maximized: boolean;
}

export const termWindows = writable<TermWindow[]>([]);

let nextKey = 1;
let topZ = 1;

/** Open editor windows allowed at once. */
const EDITOR_LIMIT = 8;

const DEFAULT_W = 864; // 720 + 20%
const DEFAULT_H = 528; // 440 + 20%
const CASCADE = 28;

/**
 * Opens a floating terminal to a server. Refuses with a toast when this page already holds the per-server
 * limit; terminals opened elsewhere are reported by the server (close code 4029) and handled by the window.
 */
export function openTermWindow(serverId: string, serverName: string, asUser?: string): void {
  const list = get(termWindows);
  const open = asUser ? list.find((w) => w.kind === 'terminal' && w.serverId === serverId && w.asUser === asUser) : undefined;
  if (open) {
    focusTermWindow(open.key);
    return;
  }
  if (list.filter((w) => w.kind === 'terminal' && w.serverId === serverId).length >= TERM_LIMIT) {
    showToast(t('term.limit'));
    return;
  }
  addWindow({ kind: 'terminal', serverId, name: termWindowName(serverName, asUser), serverName, asUser });
}

/** The title of a terminal window: the server, or user@server when the shell runs as another account. */
function termWindowName(serverName: string, asUser?: string): string {
  return asUser ? `${asUser}@${serverName}` : serverName;
}

/** Switches the account of a terminal window; the shell is started again as that account ('' is root). */
export function setTermUser(key: number, user: string): void {
  const win = get(termWindows).find((w) => w.key === key);
  if (!win || win.kind !== 'terminal') return;
  updateTermWindow(key, { asUser: user || undefined, name: termWindowName(win.serverName ?? win.name, user || undefined) });
}

/** Opens a file in a floating editor; an already open file of the same server is brought to the front. */
export function openEditorWindow(serverId: string, serverName: string, path: string, name: string, text: string, mtime: number): void {
  const list = get(termWindows);
  const open = list.find((w) => w.kind === 'editor' && w.serverId === serverId && w.path === path);
  if (open) {
    focusTermWindow(open.key);
    return;
  }
  if (list.filter((w) => w.kind === 'editor').length >= EDITOR_LIMIT) {
    showToast(t('edwin.limit'));
    return;
  }
  addWindow({ kind: 'editor', serverId, name, path, serverName, initialText: text, initialMtime: mtime });
}

/** Closes the editor windows of a file (it was deleted). */
export function closeEditorWindows(serverId: string, path: string): void {
  termWindows.update((list) => list.filter((w) => !(w.kind === 'editor' && w.serverId === serverId && w.path === path)));
}

function addWindow(init: Pick<TermWindow, 'kind' | 'serverId' | 'name'> & Partial<TermWindow>): void {
  const list = get(termWindows);
  const vw = window.innerWidth;
  const vh = window.innerHeight;
  const editor = init.kind === 'editor';
  const w = Math.min(editor ? 760 : DEFAULT_W, vw - 32);
  const h = Math.min(editor ? 520 : DEFAULT_H, vh - 96);
  const step = (list.length % 8) * CASCADE;
  const x = Math.max(16, Math.min(Math.round((vw - w) / 2) + step - 3 * CASCADE, vw - w - 16));
  const y = Math.max(16, Math.min(72 + step, vh - h - 16));
  termWindows.set([...list, { x, y, w, h, key: nextKey++, z: ++topZ, minimized: false, maximized: false, ...init }]);
}

export function updateTermWindow(key: number, patch: Partial<TermWindow>): void {
  termWindows.update((list) => list.map((w) => (w.key === key ? { ...w, ...patch } : w)));
}

/** Brings a window to the front (and restores it if minimized). */
export function focusTermWindow(key: number): void {
  const win = get(termWindows).find((w) => w.key === key);
  if (!win) return;
  if (win.z === topZ && !win.minimized) return;
  updateTermWindow(key, { z: ++topZ, minimized: false });
}

export function closeTermWindow(key: number): void {
  termWindows.update((list) => list.filter((w) => w.key !== key));
}

export function closeAllTermWindows(): void {
  termWindows.set([]);
}
