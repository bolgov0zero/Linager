import { derived, get, writable } from 'svelte/store';
import ru from './locales/ru.json';
import en from './locales/en.json';
import ruServer from './locales/ru.server.json';
import enServer from './locales/en.server.json';
import ruTerm from './locales/ru.term.json';
import enTerm from './locales/en.term.json';
import ruMgmt from './locales/ru.mgmt.json';
import enMgmt from './locales/en.mgmt.json';
import ruFiles from './locales/ru.files.json';
import enFiles from './locales/en.files.json';
import ruNet from './locales/ru.net.json';
import enNet from './locales/en.net.json';
import ruFw from './locales/ru.fw.json';
import enFw from './locales/en.fw.json';
import ruUpd from './locales/ru.upd.json';
import enUpd from './locales/en.upd.json';
import ruAudit from './locales/ru.audit.json';
import ruSettings from './locales/ru.settings.json';
import ruSec from './locales/ru.sec.json';
import ruDisk from './locales/ru.disk.json';
import enAudit from './locales/en.audit.json';
import enSettings from './locales/en.settings.json';
import enSec from './locales/en.sec.json';
import enDisk from './locales/en.disk.json';

export type Locale = 'ru' | 'en';
export type Vars = Record<string, string | number>;

const dictionaries: Record<Locale, Record<string, string>> = {
  ru: { ...ru, ...ruServer, ...ruTerm, ...ruMgmt, ...ruFiles, ...ruNet, ...ruFw, ...ruUpd, ...ruAudit, ...ruSettings, ...ruSec, ...ruDisk },
  en: { ...en, ...enServer, ...enTerm, ...enMgmt, ...enFiles, ...enNet, ...enFw, ...enUpd, ...enAudit, ...enSettings, ...enSec, ...enDisk },
};
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

export function t(key: string, current: Locale = get(locale), vars?: Vars): string {
  const raw = dictionaries[current][key] ?? dictionaries[DEFAULT_LOCALE][key] ?? key;
  if (!vars) return raw;
  return raw.replace(/\{(\w+)\}/g, (match: string, name: string) =>
    name in vars ? String(vars[name]) : match,
  );
}

// Reactive translator for templates: `{$tr('key')}` re-renders on locale change.
export const tr = derived(locale, ($locale) => (key: string, vars?: Vars) => t(key, $locale, vars));

document.documentElement.lang = get(locale);
