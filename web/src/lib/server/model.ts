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

/** 8x8 pixel icons of the tabs, one string per row; "1" is a filled square. */
export const TAB_ICONS: Record<TabId, readonly string[]> = {
  overview: ['11100111', '10100101', '11100111', '00000000', '11100111', '10100101', '11100111', '00000000'],
  metrics: ['00000001', '00000001', '00010001', '00010101', '01010101', '01010101', '01010101', '11111111'],
  terminal: ['00000000', '10000000', '01000000', '00100000', '01000000', '10000000', '00011110', '00000000'],
  desktop: ['11111111', '10000001', '10111101', '10111101', '10000001', '11111111', '00011000', '01111110'],
  services: ['00000000', '11001111', '11001111', '00000000', '11110011', '11110011', '00000000', '00000000'],
  processes: ['01010100', '01111110', '11000011', '01000010', '11000011', '01111110', '00101010', '00000000'],
  logs: ['00000000', '11111100', '00000000', '11111111', '00000000', '11110000', '00000000', '11111111'],
  updates: ['00011000', '00011000', '01011010', '00111100', '00011000', '00000000', '11111111', '11111111'],
  files: ['00000000', '11110000', '11111111', '10000001', '10000001', '10000001', '11111111', '00000000'],
  disk: ['00000000', '11111111', '10000001', '10000001', '11111111', '10000101', '11111111', '00000000'],
  network: ['00011000', '00011000', '00011000', '11111111', '10011001', '10011001', '11011011', '11011011'],
  firewall: ['11101110', '11101110', '00000000', '10111011', '10111011', '00000000', '11101110', '11101110'],
  security: ['01111110', '11111111', '11100111', '11100111', '11100111', '01100110', '00111100', '00011000'],
  users: ['00111100', '01111110', '01111110', '00111100', '00000000', '11111111', '11111111', '11111111'],
  docker: ['00110000', '00110000', '11011011', '11011011', '00000000', '11111111', '11111111', '01111110'],
};
