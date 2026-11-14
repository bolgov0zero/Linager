import { ApiError, type UpdateStep } from '../../api';
import type { Locale } from '../../i18n';

/** Translator passed down from components (the `$tr` store value). */
export type Tr = (key: string, vars?: Record<string, string | number>) => string;

export const SERVER_STEPS: UpdateStep['id'][] = ['verify', 'backup', 'stop', 'replace', 'migrate', 'start'];
export const AGENT_STEPS = ['download', 'verify', 'backup', 'replace', 'restart'] as const;
/** A finished server run stays visible for this many seconds. */
export const RUN_VISIBLE_SEC = 600;

const bcp47 = (locale: Locale): string => (locale === 'ru' ? 'ru-RU' : 'en-US');

/** Unix seconds to "сегодня, 09:58" or "14 нояб., 02:00". Zero means never. */
export function fmtDateTime(ts: number, tr: Tr, locale: Locale): string {
  if (!ts) return '—';
  const date = new Date(ts * 1000);
  const time = date.toLocaleTimeString(bcp47(locale), { hour: '2-digit', minute: '2-digit' });
  if (date.toDateString() === new Date().toDateString()) return `${tr('upd.today')}, ${time}`;
  return `${fmtDate(ts, locale)}, ${time}`;
}

/** Unix seconds to "28 окт.". Zero means never. */
export function fmtDate(ts: number, locale: Locale): string {
  if (!ts) return '—';
  return new Date(ts * 1000).toLocaleDateString(bcp47(locale), { day: 'numeric', month: 'short' });
}

export function fmtBytes(bytes: number, tr: Tr): string {
  if (bytes <= 0) return '—';
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} ${tr('upd.unit.kb')}`;
  if (bytes < 1024 ** 3) return `${(bytes / 1024 / 1024).toFixed(1)} ${tr('upd.unit.mb')}`;
  return `${(bytes / 1024 ** 3).toFixed(2)} ${tr('upd.unit.gb')}`;
}

export function errorText(error: unknown): string {
  if (error instanceof ApiError && error.serverMessage) return error.serverMessage;
  if (error instanceof Error) return error.message;
  return String(error);
}

/** True when the API did not answer normally: no connection, or a proxy 502/503/504 during a restart. */
export function isUnreachable(error: unknown): boolean {
  return error instanceof ApiError && [0, 502, 503, 504].includes(error.status);
}
