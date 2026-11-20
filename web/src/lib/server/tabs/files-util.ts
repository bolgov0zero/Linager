import { ApiError, type FileEntry } from '../../../api';
export { sortEntries, type SortDir, type SortKey } from './files-sort';

export type Translate = (key: string, vars?: Record<string, string | number>) => string;

/** Joins a directory and a child name. The root directory is "/". */
export function joinPath(dir: string, name: string): string {
  return `${dir.replace(/\/+$/, '')}/${name}`;
}

/** Parent of a directory; the parent of "/" is "/". */
export function parentPath(dir: string): string {
  const base = dir.replace(/\/+$/, '');
  const i = base.lastIndexOf('/');
  return i <= 0 ? '/' : base.slice(0, i);
}

export interface Crumb {
  name: string;
  path: string;
}

/** Path segments with the absolute path each one leads to. */
export function crumbs(dir: string): Crumb[] {
  const out: Crumb[] = [];
  let acc = '';
  for (const name of dir.split('/').filter(Boolean)) {
    acc = `${acc}/${name}`;
    out.push({ name, path: acc });
  }
  return out;
}

/** Returns a translation key when the name cannot be used, otherwise null. */
export function nameError(raw: string): string | null {
  const name = raw.trim();
  if (!name) return 'files.err.nameEmpty';
  if (name.includes('/') || name.includes('\0')) return 'files.err.nameSlash';
  if (name === '.' || name === '..') return 'files.err.nameReserved';
  return null;
}

/** 503 means the server is offline, 501 means the agent module is disabled. */
export function unavailable(error: unknown): 'offline' | 'disabled' | null {
  if (error instanceof ApiError) {
    if (error.status === 503) return 'offline';
    if (error.status === 501) return 'disabled';
  }
  return null;
}

/** Human-readable text for a failed call; prefers the server's own message. */
export function errorText(error: unknown, tr: Translate): string {
  if (error instanceof ApiError) {
    if (error.status === 0) return tr('common.networkError');
    if (error.status === 503) return tr('files.offline');
    if (error.status === 501) return tr('files.disabled');
    if (error.serverMessage) return error.serverMessage;
  }
  return tr('common.genericError');
}

/** Errors specific to opening a file in the editor. */
export function openErrorText(error: unknown, tr: Translate): string {
  if (error instanceof ApiError) {
    if (error.status === 413) return tr('files.err.tooBig');
    if (error.status === 415) return tr('files.err.binary');
  }
  return errorText(error, tr);
}

export function isStatus(error: unknown, status: number): boolean {
  return error instanceof ApiError && error.status === status;
}
