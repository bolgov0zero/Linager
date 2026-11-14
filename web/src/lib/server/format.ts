import type { Period } from '../../api';
import type { Vars } from '../../i18n';

export type Translate = (key: string, vars?: Vars) => string;

const UNITS = ['B', 'KB', 'MB', 'GB', 'TB'];

/** Binary units (1 KB = 1024 B). Returns a dash for missing values. */
export function formatBytes(value: number | null | undefined): string {
  if (value === null || value === undefined || !Number.isFinite(value)) return '—';
  let scaled = Math.max(0, value);
  let unit = 0;
  while (scaled >= 1024 && unit < UNITS.length - 1) {
    scaled /= 1024;
    unit++;
  }
  const digits = unit === 0 || scaled >= 100 ? 0 : 1;
  return `${scaled.toFixed(digits)} ${UNITS[unit]}`;
}

export function formatRate(value: number | null | undefined): string {
  const text = formatBytes(value);
  return text === '—' ? text : `${text}/s`;
}

export function formatPct(value: number | null | undefined): string {
  if (value === null || value === undefined || !Number.isFinite(value)) return '—';
  return `${Math.round(value)}%`;
}

export function formatLoad(values: readonly number[]): string {
  return values.map((v) => v.toFixed(2)).join(' · ');
}

export function formatUptime(seconds: number, tr: Translate): string {
  const days = Math.floor(seconds / 86400);
  const hours = Math.floor((seconds % 86400) / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  if (days > 0) return tr('srv.dur.dh', { d: days, h: hours });
  if (hours > 0) return tr('srv.dur.hm', { h: hours, m: minutes });
  return tr('srv.dur.m', { m: minutes });
}

/** Accepts unix seconds (history, samples) or an ISO string (last_seen_at). */
export function formatDateTime(value: number | string, lang: string): string {
  const date = typeof value === 'number' ? new Date(value * 1000) : new Date(value);
  if (Number.isNaN(date.getTime())) return String(value);
  return date.toLocaleString(lang, { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' });
}

/** Axis label: clock time for the short periods, date for the long ones. */
export function formatTick(ts: number, period: Period, lang: string): string {
  const date = new Date(ts * 1000);
  if (period === '1h' || period === '24h') {
    return date.toLocaleTimeString(lang, { hour: '2-digit', minute: '2-digit' });
  }
  return date.toLocaleDateString(lang, { day: 'numeric', month: 'short' });
}

/** Human step: "5 с", "1 мин", "1 ч". */
export function formatStep(seconds: number, tr: Translate): string {
  if (seconds < 60) return tr('srv.step.s', { n: seconds });
  if (seconds % 3600 === 0) return tr('srv.step.h', { n: seconds / 3600 });
  return tr('srv.step.m', { n: seconds / 60 });
}
