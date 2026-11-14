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
  { id: 'network', key: 'srv.tab.network' },
  { id: 'firewall', key: 'srv.tab.firewall' },
  { id: 'docker', key: 'srv.tab.docker' },
  { id: 'updates', key: 'srv.tab.updates' },
  { id: 'processes', key: 'srv.tab.processes' },
  { id: 'logs', key: 'srv.tab.logs' },
] as const;

export type TabId = (typeof TABS)[number]['id'];
