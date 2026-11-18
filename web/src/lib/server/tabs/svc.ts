import { ApiError, type ServiceAction, type ServiceUnit } from '../../../api';
import type { Translate } from '../format';

/** Units whose stop, restart or disable needs an explicit confirmation. */
const PROTECTED = new Set([
  'ssh', 'sshd', 'linager-agent',
  // Windows services (compared in lower case): the agent, remote desktop, remote management and the network core
  'linageragent', 'termservice', 'winrm', 'rpcss', 'rpceptmapper', 'dcomlaunch', 'lanmanserver', 'lanmanworkstation', 'dhcp', 'dnscache',
  'nlasvc', 'netlogon', 'bfe', 'mpssvc', 'eventlog', 'winmgmt', 'cryptsvc', 'samss',
]);

export type StateKind = 'running' | 'failed' | 'stopped' | 'other';
export type ServiceFilter = 'all' | 'running' | 'stopped' | 'failed';

export function unitBase(name: string): string {
  return name.replace(/\.service$/, '');
}

export function stateKind(unit: ServiceUnit): StateKind {
  if (unit.active === 'failed' || unit.sub === 'failed') return 'failed';
  if (unit.active === 'active') return 'running';
  if (unit.active === 'inactive') return 'stopped';
  return 'other';
}

export function isDangerous(unit: string, action: ServiceAction): boolean {
  return (action === 'stop' || action === 'restart' || action === 'disable') && PROTECTED.has(unitBase(unit).toLowerCase());
}

export function confirmText(unit: string, action: ServiceAction, tr: Translate): string {
  const base = unitBase(unit).toLowerCase();
  if (base === 'linager-agent' || base === 'linageragent') return tr('svc.confirm.agent');
  if (base === 'ssh' || base === 'sshd') return tr('svc.confirm.ssh');
  return tr('svc.confirm.generic', { action: tr(`svc.act.${action}`), unit });
}

const RANK: Record<StateKind, number> = { failed: 0, running: 1, other: 2, stopped: 3 };

/** Applies the state filter and the name/description search; failed units come first. */
export function visibleServices(list: ServiceUnit[], filter: ServiceFilter, query: string): ServiceUnit[] {
  const q = query.trim().toLowerCase();
  return list
    .filter((u) => {
      const kind = stateKind(u);
      if (filter !== 'all' && kind !== filter) return false;
      if (!q) return true;
      return u.name.toLowerCase().includes(q) || u.description.toLowerCase().includes(q);
    })
    .sort((a, b) => RANK[stateKind(a)] - RANK[stateKind(b)] || a.name.localeCompare(b.name));
}

/** 503 means the server is offline, 501 means the agent module is disabled. */
export function unavailableKind(error: unknown): 'offline' | 'disabled' | null {
  if (error instanceof ApiError) {
    if (error.status === 503) return 'offline';
    if (error.status === 501) return 'disabled';
  }
  return null;
}

/** The server asks for confirmation with 409 when the message mentions it. */
export function needsConfirmation(error: unknown): boolean {
  return error instanceof ApiError && error.status === 409 && /confirm|подтверд/i.test(error.serverMessage ?? '');
}

/** Human-readable text for a failed API call. */
export function errorText(error: unknown, tr: Translate): string {
  if (error instanceof ApiError) {
    if (error.status === 0) return tr('common.networkError');
    if (error.status === 503) return tr('svc.offline');
    if (error.status === 501) return tr('svc.disabled');
    if (error.serverMessage) return error.serverMessage;
  }
  return tr('common.genericError');
}
