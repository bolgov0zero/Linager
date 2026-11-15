export type SettingsSection = 'profile' | 'users' | 'updates' | 'audit' | 'snippets';

export const SETTINGS_SECTIONS: SettingsSection[] = ['profile', 'users', 'updates', 'audit', 'snippets'];

export function isSection(value: string): value is SettingsSection {
  return (SETTINGS_SECTIONS as string[]).includes(value);
}
