import { writable } from 'svelte/store';
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
