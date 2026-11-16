export type SettingsSection = 'profile' | 'users' | 'groups' | 'snippets' | 'access' | 'updates' | 'audit';

export const SETTINGS_SECTIONS: SettingsSection[] = ['profile', 'users', 'groups', 'snippets', 'access', 'updates', 'audit'];

export function isSection(value: string): value is SettingsSection {
  return (SETTINGS_SECTIONS as string[]).includes(value);
}
