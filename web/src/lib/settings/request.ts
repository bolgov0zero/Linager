import { writable } from 'svelte/store';
import type { SettingsSection } from './sections';

/** Components ask the app to open the settings on a section ("Manage snippets" in the terminal, and so on). */
export const settingsRequest = writable<{ section: SettingsSection; seq: number } | null>(null);

let seq = 0;

export function requestSettings(section: SettingsSection): void {
  settingsRequest.set({ section, seq: ++seq });
}
