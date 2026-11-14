import { get, writable } from 'svelte/store';
import { t } from '../../i18n';
import { showToast } from '../toast';

/** Simultaneous terminals per server allowed by the server (server/api_mgmt.go termLimit). */
export const TERM_LIMIT = 3;

export interface TermWindow {
  key: number;
  serverId: string;
  name: string;
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

const DEFAULT_W = 720;
const DEFAULT_H = 440;
const CASCADE = 28;

/**
 * Opens a floating terminal to a server. Refuses with a toast when this page already holds the per-server
 * limit; terminals opened elsewhere are reported by the server (close code 4029) and handled by the window.
 */
export function openTermWindow(serverId: string, name: string): void {
  const list = get(termWindows);
  if (list.filter((w) => w.serverId === serverId).length >= TERM_LIMIT) {
    showToast(t('term.limit'));
    return;
  }
  const vw = window.innerWidth;
  const vh = window.innerHeight;
  const w = Math.min(DEFAULT_W, vw - 32);
  const h = Math.min(DEFAULT_H, vh - 96);
  const step = (list.length % 8) * CASCADE;
  const x = Math.max(16, Math.min(Math.round((vw - w) / 2) + step - 3 * CASCADE, vw - w - 16));
  const y = Math.max(16, Math.min(72 + step, vh - h - 16));
  termWindows.set([
    ...list,
    { key: nextKey++, serverId, name, x, y, w, h, z: ++topZ, minimized: false, maximized: false },
  ]);
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
