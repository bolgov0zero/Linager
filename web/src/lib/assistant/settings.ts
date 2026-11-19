import { get, writable } from 'svelte/store';
import { assistantApi, type AssistSettings } from '../../api';

/** The picture settings of every computer whose Assistant is on screen, as last read from the panel. Shared by the screen (the pointer) and the line under it (the limits). */
export const assistSettings = writable<Record<string, AssistSettings>>({});

/** Puts known settings in (after a save, or after a read). */
export function setAssistSettings(serverId: string, settings: AssistSettings): void {
  assistSettings.update((all) => ({ ...all, [serverId]: settings }));
}

/** Reads the settings again; a failure leaves what is known. */
export async function refreshAssistSettings(serverId: string): Promise<void> {
  try {
    setAssistSettings(serverId, await assistantApi.settings(serverId));
  } catch {
    // the limits are just not shown
  }
}

const INFO_KEY = 'linager.assist.info';

function readInfoShown(): boolean {
  try {
    return localStorage.getItem(INFO_KEY) !== '0';
  } catch {
    return true;
  }
}

/** Whether the line under the screen (resolution, colours, frames, traffic) is shown. On by default; one choice for every Assistant window and tab, remembered in the browser. */
export const assistInfoShown = writable<boolean>(readInfoShown());

export function toggleAssistInfo(): void {
  const next = !get(assistInfoShown);
  assistInfoShown.set(next);
  try {
    localStorage.setItem(INFO_KEY, next ? '1' : '0');
  } catch {
    // storage unavailable: the choice just is not remembered
  }
}
