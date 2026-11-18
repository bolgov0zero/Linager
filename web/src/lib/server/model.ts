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

/** One client-side live sample; null when the value is unknown. */
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
  { id: 'desktop', key: 'srv.tab.desktop' },
  { id: 'files', key: 'srv.tab.files' },
  { id: 'disk', key: 'srv.tab.disk' },
  { id: 'services', key: 'srv.tab.services' },
  { id: 'processes', key: 'srv.tab.processes' },
  { id: 'network', key: 'srv.tab.network' },
  { id: 'firewall', key: 'srv.tab.firewall' },
  { id: 'security', key: 'srv.tab.security' },
  { id: 'users', key: 'srv.tab.users' },
  { id: 'docker', key: 'srv.tab.docker' },
  { id: 'updates', key: 'srv.tab.updates' },
  { id: 'logs', key: 'srv.tab.logs' },
] as const;

export type TabId = (typeof TABS)[number]['id'];

/** Agent module that backs a tab; tabs not listed here (overview, metrics) are always shown. */
const TAB_MODULE: Partial<Record<TabId, string>> = {
  terminal: 'terminal',
  desktop: 'desktop',
  files: 'files',
  disk: 'disk',
  services: 'services',
  network: 'network',
  firewall: 'firewall',
  security: 'security',
  users: 'users',
  docker: 'docker',
  updates: 'apt',
  processes: 'processes',
  logs: 'logs',
};

/**
 * Tabs to show for the reported agent modules. An empty list means the agent did not report
 * its modules (old agent): all tabs stay except Docker, Security and Disk, which need the module to be reported.
 */
export function visibleTabs(modules: readonly string[], allowed: readonly string[] | null = null): (typeof TABS)[number][] {
  return TABS.filter((tab) => {
    const module = TAB_MODULE[tab.id];
    if (!module) return true;
    // Windows Update has its own agent module behind the same tab
    if (tab.id === 'updates' && modules.includes('winupdate') && (allowed === null || allowed.includes('apt'))) return true;
    // an operator only gets the modules that were given (an administrator: null), an auditor none
    if (allowed !== null && !allowed.includes(module)) return false;
    if (modules.length === 0) return module !== 'docker' && module !== 'security' && module !== 'disk' && module !== 'users';
    return modules.includes(module);
  });
}

/** Sidebar groups of the server page; every tab belongs to exactly one group, in the order shown. */
export const TAB_GROUPS: readonly { id: string; key: string; tabs: readonly TabId[] }[] = [
  { id: 'general', key: 'srv.group.general', tabs: ['overview', 'metrics', 'terminal', 'desktop'] },
  { id: 'system', key: 'srv.group.system', tabs: ['services', 'processes', 'logs', 'updates'] },
  { id: 'fs', key: 'srv.group.fs', tabs: ['files', 'disk'] },
  { id: 'net', key: 'srv.group.net', tabs: ['network', 'firewall', 'security', 'users'] },
  { id: 'apps', key: 'srv.group.apps', tabs: ['docker'] },
];

/** The visible tabs split into the sidebar groups; a group with no visible tab is dropped. */
export function groupTabs(tabs: readonly (typeof TABS)[number][]): { id: string; key: string; tabs: (typeof TABS)[number][] }[] {
  return TAB_GROUPS.map((g) => ({
    id: g.id,
    key: g.key,
    tabs: g.tabs.flatMap((id) => tabs.filter((t) => t.id === id)),
  })).filter((g) => g.tabs.length > 0);
}

/** 9x9 pixel icons of the tabs, one string per row; "1" is a filled square. Drawn so that one square is 2 px at 18 px, which keeps them sharp on any screen. */
export const TAB_ICONS: Record<TabId, readonly string[]> = {
  overview: ['111101111', '101101101', '101101101', '111101111', '000000000', '111101111', '101101101', '101101101', '111101111'],
  metrics: ['000000001', '000000001', '000000101', '000000101', '000010101', '001010101', '101010101', '101010101', '111111111'],
  terminal: ['000000000', '110000000', '011000000', '001100000', '000110000', '001100000', '011000000', '110001111', '000001111'],
  desktop: ['111111111', '100000001', '101111101', '101111101', '100000001', '111111111', '000010000', '000010000', '011111110'],
  services: ['000000000', '110111111', '110111111', '000000000', '110111111', '110111111', '000000000', '110111111', '110111111'],
  processes: ['001010100', '011111110', '110000011', '010111010', '110111011', '010111010', '110000011', '011111110', '001010100'],
  logs: ['111111111', '111111111', '000000000', '111111000', '111111000', '000000000', '111111111', '111111111', '000000000'],
  updates: ['000111000', '000111000', '000111000', '111111111', '011111110', '001111100', '000111000', '100000001', '111111111'],
  files: ['111110000', '100010000', '111111111', '100000001', '100000001', '100000001', '100000001', '100000001', '111111111'],
  disk: ['000000000', '111111111', '100000001', '100000001', '111111111', '100000001', '100000101', '100000001', '111111111'],
  network: ['000111000', '000111000', '000010000', '111111111', '100010001', '100010001', '110111011', '110111011', '110111011'],
  firewall: ['000000000', '111011111', '111011111', '000000000', '110111011', '110111011', '000000000', '111011111', '111011111'],
  security: ['111111111', '111101111', '111101111', '111101111', '111101111', '011101110', '001101100', '000111000', '000010000'],
  users: ['000111000', '001111100', '001111100', '000111000', '000000000', '011111110', '111111111', '111111111', '111111111'],
  docker: ['000000000', '000110000', '000110000', '110110110', '110110110', '000000000', '111111111', '111111111', '011111110'],
};
