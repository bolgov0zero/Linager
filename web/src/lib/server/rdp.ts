import { writable } from 'svelte/store';
import type { Sample } from '../../api';

/**
 * What the administrator just did to the remote desktop of a Windows computer (true = turned off), kept for a few
 * seconds: the agent reports the state with its samples (`desktop_off`), and until the next one arrives this makes the
 * "Desktop" tab and menu items appear or disappear at once.
 */
export const rdpOverride = writable<Record<string, boolean>>({});

const HOLD_MS = 15000;

export function setRdpOff(serverId: string, off: boolean): void {
  rdpOverride.update((m) => ({ ...m, [serverId]: off }));
  setTimeout(() => {
    rdpOverride.update((m) => {
      const { [serverId]: _gone, ...rest } = m;
      return rest;
    });
  }, HOLD_MS);
}

/** The remote desktop is off or impossible (Windows Home): there is nothing to open. An old agent reports nothing: shown. */
export function desktopHidden(serverId: string, sample: Sample | null | undefined, override: Record<string, boolean>): boolean {
  const o = override[serverId];
  return o !== undefined ? o : sample?.desktop_off === true;
}
