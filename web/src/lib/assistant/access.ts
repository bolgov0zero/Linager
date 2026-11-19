import { writable } from 'svelte/store';

/** What the person who is signed in may do: the Dispatcher of the Assistant needs the modules "processes" and "services". */
export const myAccess = writable<{ role: string; modules: string[] }>({ role: 'auditor', modules: [] });

export interface DispatcherTabs {
  proc: boolean;
  svc: boolean;
}

/** An administrator may everything, an operator what was given, an auditor nothing (the actions change the computer). */
export function dispatcherTabs(role: string, modules: readonly string[]): DispatcherTabs {
  const can = (m: string): boolean => role === 'admin' || (role === 'operator' && modules.includes(m));
  return { proc: can('processes'), svc: can('services') };
}
