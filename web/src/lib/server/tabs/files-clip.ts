import { writable } from 'svelte/store';

/** What was cut or copied in the file manager; it survives moving between folders and tabs of the same server. */
export interface Clip {
  mode: 'cut' | 'copy';
  serverId: string;
  dir: string;
  names: string[];
}

export const clip = writable<Clip | null>(null);
