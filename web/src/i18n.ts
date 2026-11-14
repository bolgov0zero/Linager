import { get, writable } from 'svelte/store';
import ru from './locales/ru.json';
import en from './locales/en.json';

export type Locale = 'ru' | 'en';

const dictionaries: Record<Locale, Record<string, string>> = { ru, en };
const STORAGE_KEY = 'linager.lang';
const DEFAULT_LOCALE: Locale = 'ru';

function readStored(): Locale {
  try {
    const value = localStorage.getItem(STORAGE_KEY);
    if (value === 'ru' || value === 'en') return value;
  } catch {
    // storage unavailable (private mode, blocked) - fall back to default
  }
  return DEFAULT_LOCALE;
}

export const locale = writable<Locale>(readStored());

export function setLocale(next: Locale): void {
  locale.set(next);
  try {
    localStorage.setItem(STORAGE_KEY, next);
  } catch {
    // ignore storage errors
  }
  document.documentElement.lang = next;
}

export function t(key: string, current: Locale = get(locale)): string {
  return dictionaries[current][key] ?? dictionaries[DEFAULT_LOCALE][key] ?? key;
}

document.documentElement.lang = get(locale);
