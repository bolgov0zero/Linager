/** A point of a bar chart: average bar plus a peak mark. */
export interface ChartPoint {
  ts: number; // unix seconds, start of the bar
  value: number;
  peak: number;
}

/** A marker on the time axis (reboot, service failure, ...). */
export interface ChartEvent {
  ts: number; // unix seconds
  kind: string;
}

/** One client-side live sample; null when the value is unknown (seeded from spark, CPU only). */
export interface LiveSample {
  cpu: number | null;
  mem: number | null;
  rx: number | null;
  tx: number | null;
}

/** Number of live samples kept for the overview bars. */
export const HISTORY_LIMIT = 60;

export const TABS = [
  { id: 'overview', key: 'srv.tab.overview' },
  { id: 'metrics', key: 'srv.tab.metrics' },
  { id: 'terminal', key: 'srv.tab.terminal' },
  { id: 'files', key: 'srv.tab.files' },
  { id: 'services', key: 'srv.tab.services' },
  { id: 'processes', key: 'srv.tab.processes' },
  { id: 'network', key: 'srv.tab.network' },
  { id: 'firewall', key: 'srv.tab.firewall' },
  { id: 'security', key: 'srv.tab.security' },
  { id: 'docker', key: 'srv.tab.docker' },
  { id: 'updates', key: 'srv.tab.updates' },
  { id: 'logs', key: 'srv.tab.logs' },
] as const;

export type TabId = (typeof TABS)[number]['id'];

/** Agent module that backs a tab; tabs not listed here (overview, metrics) are always shown. */
const TAB_MODULE: Partial<Record<TabId, string>> = {
  terminal: 'terminal',
  files: 'files',
  services: 'services',
  network: 'network',
  firewall: 'firewall',
  security: 'security',
  docker: 'docker',
  updates: 'apt',
  processes: 'processes',
  logs: 'logs',
};

/**
 * Tabs to show for the reported agent modules. An empty list means the agent did not report
 * its modules (old agent): all tabs stay except Docker and Security, which need the module to be reported.
 */
export function visibleTabs(modules: readonly string[]): (typeof TABS)[number][] {
  return TABS.filter((tab) => {
    const module = TAB_MODULE[tab.id];
    if (!module) return true;
    if (modules.length === 0) return module !== 'docker' && module !== 'security';
    return modules.includes(module);
  });
}
