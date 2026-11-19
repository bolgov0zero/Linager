import type { ProcessInfo, ServiceUnit } from '../../api';

/** The Dispatcher of the Assistant: sorting, names and the actions of a row. Pure, so that it can be tested. */

export type SortDir = 'asc' | 'desc';
export interface SortState<K extends string> {
  key: K;
  dir: SortDir;
}

export type ProcKey = 'name' | 'pid' | 'user' | 'cpu' | 'ram';
export type SvcKey = 'name' | 'state' | 'auto';

/** A click on a column: the same column turns its order over, another one starts as in the Task Manager (text up, numbers down). */
export function nextSort<K extends string>(cur: SortState<K>, key: K, text: boolean): SortState<K> {
  if (cur.key === key) return { key, dir: cur.dir === 'asc' ? 'desc' : 'asc' };
  return { key, dir: text ? 'asc' : 'desc' };
}

/** The executable name of a command line: `"C:\Program Files\x\app.exe" -v` gives `app.exe`. */
export function procName(cmd: string): string {
  const c = cmd.trim();
  let exe = c;
  if (c.startsWith('"')) {
    const j = c.indexOf('"', 1);
    if (j > 0) exe = c.slice(1, j);
  } else {
    const i = c.search(/\s/);
    if (i >= 0) exe = c.slice(0, i);
  }
  const cut = Math.max(exe.lastIndexOf('\\'), exe.lastIndexOf('/'));
  return cut >= 0 ? exe.slice(cut + 1) : exe;
}

const cmp = (a: number | string, b: number | string): number => (a < b ? -1 : a > b ? 1 : 0);

export function sortProcs(list: ProcessInfo[], s: SortState<ProcKey>): ProcessInfo[] {
  const d = s.dir === 'asc' ? 1 : -1;
  const val = (p: ProcessInfo): number | string => {
    switch (s.key) {
      case 'name':
        return procName(p.cmd).toLowerCase();
      case 'pid':
        return p.pid;
      case 'user':
        return p.user.toLowerCase();
      case 'cpu':
        return p.cpu;
      default:
        return p.rss;
    }
  };
  return [...list].sort((a, b) => cmp(val(a), val(b)) * d || cmp(procName(a.cmd).toLowerCase(), procName(b.cmd).toLowerCase()) || a.pid - b.pid);
}

export type StateKind = 'running' | 'failed' | 'stopped' | 'other';

/** The same reading of a service as the "Services" tab has (server/tabs/svc.ts); it is repeated here so that this file has no imports to run in a test. */
export function stateKind(unit: Pick<ServiceUnit, 'active' | 'sub'>): StateKind {
  if (unit.active === 'failed' || unit.sub === 'failed') return 'failed';
  if (unit.active === 'active') return 'running';
  if (unit.active === 'inactive') return 'stopped';
  return 'other';
}

const STATE_RANK: Record<StateKind, number> = { failed: 0, running: 1, other: 2, stopped: 3 };
const AUTO_RANK: Record<string, number> = { enabled: 0, static: 1, disabled: 2 };

export function sortServices(list: ServiceUnit[], s: SortState<SvcKey>): ServiceUnit[] {
  const d = s.dir === 'asc' ? 1 : -1;
  const val = (u: ServiceUnit): number | string => (s.key === 'name' ? u.name.toLowerCase() : s.key === 'state' ? STATE_RANK[stateKind(u)] : (AUTO_RANK[u.enabled] ?? 3));
  return [...list].sort((a, b) => cmp(val(a), val(b)) * d || a.name.localeCompare(b.name));
}

/** What the bottom bar offers for a service: a running one is restarted or stopped, any other one is started. */
export function serviceActions(u: ServiceUnit | undefined): ('start' | 'restart' | 'stop')[] {
  if (!u) return [];
  return stateKind(u) === 'running' ? ['restart', 'stop'] : ['start'];
}

/** How warm the cell is drawn, 0 (not at all) to 1: the busier the process, the warmer. */
export function heat(value: number, full: number): number {
  return value <= 0 || full <= 0 ? 0 : Math.min(1, value / full);
}
