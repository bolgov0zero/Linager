export interface Session {
  csrf: string;
  login: string;
  totp_enabled: boolean;
}

export interface Server {
  id: string;
  name: string;
  hostname: string;
  address: string;
  os: string;
  agent_version: string;
  online: boolean;
  last_seen_at: string | null;
}

export interface Enrollment {
  id: string;
  hostname: string;
  address: string;
  remote_addr: string;
  os: string;
  agent_version: string;
  key_fingerprint: string;
  verify_code: string;
  expires_at: string;
}

export interface TotpSetup {
  secret: string;
  uri: string;
}


// ---- metrics / dashboard (stage 3) ----

export interface Disk { mount: string; device: string; fs: string; total: number; used: number; pct: number }
export interface NetIface {
  iface: string; up: boolean; ip: string; mac: string; speed_mbps: number;
  rx_bps: number; tx_bps: number; rx_total: number; tx_total: number;
}
/** One live sample (every 5 s). Percentages are 0-100, bytes are bytes, rates bytes/s. */
export interface Sample {
  ts: number; // unix seconds
  cpu: number;
  mem_used: number; mem_total: number; mem_pct: number;
  swap_used: number; swap_total: number;
  disks: Disk[];
  disk_pct: number; // highest usage among real disks
  net: NetIface[];
  rx_bps: number; tx_bps: number; // sum over physical interfaces
  load: [number, number, number];
  uptime: number; boot_time: number;
  failed_services: number;
  reboot_required: boolean;
  updates: number; // apt upgradable packages, -1 = unknown
  security_updates: number;
  procs: number;
}
export interface SystemInfo {
  hostname: string; os: string; kernel: string; arch: string;
  cpu_model: string; cpu_cores: number; mem_total: number; agent_version: string;
}
/** A server on the dashboard. spark = last <=12 CPU values (5 s apart), oldest first. */
export interface ServerCard {
  id: string; name: string; address: string; os: string; agent_version: string;
  online: boolean; last_seen_at: string | null;
  sample: Sample | null; spark: number[];
}
export interface AvailableServer { id: string; name: string; address: string; os: string }
export interface ServerDetail {
  id: string; name: string; hostname: string; address: string; os: string; agent_version: string;
  online: boolean; last_seen_at: string | null; enrolled_at: string;
  system: SystemInfo | null; sample: Sample | null; spark: number[];
}

export type MetricKey = 'cpu' | 'mem' | 'disk' | 'rx' | 'tx';
export type Period = '1h' | '24h' | '7d' | '30d' | '90d' | '180d' | '360d';
export interface HistoryPoint {
  ts: number; // unix seconds, start of the bar
  cpu_avg: number; cpu_max: number;
  mem_avg: number; mem_max: number;
  disk_avg: number; disk_max: number;
  rx_avg: number; rx_max: number;
  tx_avg: number; tx_max: number;
}
export interface MetricStats { min: number; avg: number; p95: number; max: number }
export interface HistoryEvent { ts: number; kind: 'reboot' | 'service_failed' | 'offline' | 'online'; message: string }
export interface History {
  period: Period; tier: 'raw' | '1m' | '5m' | '15m' | '1h';
  step_seconds: number; // width of one bar
  from: number; to: number;
  points: HistoryPoint[];
  stats: Record<MetricKey, MetricStats>;
  events: HistoryEvent[];
}

/** Server-sent events from api/live. */
export type LiveEvent =
  | { type: 'metrics'; server_id: string; sample: Sample }
  | { type: 'status'; server_id: string; online: boolean; last_seen_at: string | null }
  | { type: 'dashboard' }; // membership/order changed or a server was added or removed

/** status 0 means the request never reached the server. */
export class ApiError extends Error {
  readonly status: number;
  readonly serverMessage: string | undefined;

  constructor(status: number, serverMessage?: string) {
    super(serverMessage ?? `HTTP ${status}`);
    this.name = 'ApiError';
    this.status = status;
    this.serverMessage = serverMessage;
  }
}

export function isUnauthorized(error: unknown): boolean {
  return error instanceof ApiError && error.status === 401;
}

// Relative paths (no leading slash) so the app works under a base path.
let csrfToken = '';

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = { Accept: 'application/json' };
  if (body !== undefined) headers['Content-Type'] = 'application/json';
  if (method !== 'GET') headers['X-CSRF-Token'] = csrfToken;

  let res: Response;
  try {
    res = await fetch(path, {
      method,
      headers,
      credentials: 'same-origin',
      body: body === undefined ? undefined : JSON.stringify(body),
    });
  } catch {
    throw new ApiError(0);
  }

  if (!res.ok) {
    let message: string | undefined;
    try {
      const data = (await res.json()) as { error?: unknown } | null;
      if (data && typeof data.error === 'string') message = data.error;
    } catch {
      // response had no JSON body
    }
    throw new ApiError(res.status, message);
  }

  const text = await res.text();
  return (text ? JSON.parse(text) : undefined) as T;
}

const enc = encodeURIComponent;

export const api = {
  async me(): Promise<Session> {
    const session = await request<Session>('GET', 'api/me');
    csrfToken = session.csrf;
    return session;
  },

  async login(login: string, password: string, totp?: string): Promise<Session> {
    const session = await request<Session>('POST', 'api/login', { login, password, totp });
    csrfToken = session.csrf;
    return session;
  },

  async logout(): Promise<void> {
    try {
      await request<void>('POST', 'api/logout');
    } finally {
      csrfToken = '';
    }
  },

  servers: () => request<Server[]>('GET', 'api/servers'),

  deleteServer: (id: string, name: string, uninstall: boolean) =>
    request<void>('DELETE', `api/servers/${enc(id)}`, { name, uninstall }),

  enrollments: () => request<Enrollment[]>('GET', 'api/enrollments'),

  approveEnrollment: (id: string, key: string) =>
    request<void>('POST', `api/enrollments/${enc(id)}/approve`, { key }),

  rejectEnrollment: (id: string) => request<void>('POST', `api/enrollments/${enc(id)}/reject`),

  totpSetup: () => request<TotpSetup>('POST', 'api/me/totp/setup'),

  totpEnable: (code: string) => request<void>('POST', 'api/me/totp/enable', { code }),

  totpDisable: (password: string) => request<void>('POST', 'api/me/totp/disable', { password }),

  dashboard: () => request<{ items: ServerCard[] }>('GET', 'api/dashboard'),
  availableServers: () => request<AvailableServer[]>('GET', 'api/dashboard/available'),
  dashboardAdd: (serverId: string) => request<void>('POST', 'api/dashboard/items', { server_id: serverId }),
  dashboardRemove: (serverId: string) => request<void>('DELETE', `api/dashboard/items/${enc(serverId)}`),
  dashboardOrder: (serverIds: string[]) => request<void>('PUT', 'api/dashboard/order', { server_ids: serverIds }),
  server: (id: string) => request<ServerDetail>('GET', `api/servers/${enc(id)}`),
  history: (id: string, period: Period) => request<History>('GET', `api/servers/${enc(id)}/metrics?period=${period}`),
  /** Relative URL for a CSV download link. */
  historyCsvUrl: (id: string, period: Period) => `api/servers/${enc(id)}/metrics.csv?period=${period}`,
};
