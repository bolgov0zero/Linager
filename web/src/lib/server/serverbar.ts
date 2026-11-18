import { writable } from 'svelte/store';
import type { ServerDetail } from '../../api';

/** What the top bar shows while a server page is open: the page fills it, the bar draws it. */
export interface ServerBar {
  detail: ServerDetail;
  online: boolean;
  isAdmin: boolean;
  /** "last seen: ..." for the badge of a server that is offline; empty when it is online or never seen. */
  lastSeen: string;
  hasNote: boolean;
  onback: () => void;
  onnote: () => void;
  ondelete: () => void;
}

export const serverBar = writable<ServerBar | null>(null);
