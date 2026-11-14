export interface Session {
  csrf: string;
  login: string;
  totp_enabled: boolean;
  /** The server requires 2FA (admin.require_totp) and it is not enabled yet: the API stays closed until it is. */
  totp_required?: boolean;
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
export interface ServiceUnit {
  name: string; description: string;
  active: string; // active | inactive | failed | activating | deactivating
  sub: string; // running | dead | exited | failed | ...
  enabled: string; // enabled | disabled | static | masked | indirect | ...
}
export type ServiceAction = 'start' | 'stop' | 'restart' | 'enable' | 'disable';
export interface AptPackage { name: string; current: string; candidate: string; security: boolean; reboot: boolean }
export interface AptList { packages: AptPackage[]; reboot_required: boolean; last_check: number }
export interface ProcessInfo { pid: number; user: string; cpu: number; mem_pct: number; rss: number; state: string; cmd: string }
export interface ProcessList { processes: ProcessInfo[]; total: number }
export type StreamKind = 'terminal' | 'logs' | 'apt' | 'docker-logs' | 'docker-pull';

// ---- files (stage 5) ----
export interface FileEntry {
  name: string; type: 'dir' | 'file' | 'link' | 'other'; size: number;
  mode: string; // octal, e.g. "0755"
  mode_str: string; // e.g. "drwxr-xr-x"
  owner: string; group: string; mtime: number; link_target?: string;
}
export interface FileList { path: string; entries: FileEntry[] }
export interface FileContent { path: string; content: string; size: number; mtime: number; mode: string }

// ---- network (stage 5) ----
export interface NetInterface {
  name: string; state: 'UP' | 'DOWN'; kind: 'physical' | 'virtual' | 'loopback';
  mac: string; mtu: number; mode: 'static' | 'dhcp' | 'manual' | 'unknown';
  addresses: string[]; gateway: string; speed_mbps: number;
  rx_bytes: number; tx_bytes: number; rx_packets: number; tx_packets: number;
  rx_errors: number; tx_errors: number; rx_dropped: number; tx_dropped: number;
  primary: boolean; // carries the agent's connection
  editable: boolean;
}
export interface NetRoute { dst: string; gateway: string; dev: string; metric: number; protocol: string }
export interface NetChange { iface: string; mode: 'static' | 'dhcp'; address: string; gateway: string; mtu: number; up: boolean }
export interface NetworkDraft { changes: NetChange[] }
export interface PendingChange { id: string; kind: 'network' | 'firewall'; deadline: number /* unix seconds */ }
export interface NetworkInfo {
  backend: 'ifupdown' | 'netplan' | 'unknown';
  editable: boolean; // false for netplan in this version
  interfaces: NetInterface[];
  dns: { nameservers: string[]; search: string[] };
  routes: NetRoute[];
  config_preview: string;
  pending: PendingChange | null;
  draft: NetworkDraft | null;
}
export interface FieldError { iface: string; field: string; message: string }
export interface ApplyWarning { code: 'primary_iface' | 'ssh_port' | 'agent_link'; message: string }

// ---- firewall (stage 5) ----
export interface FwRule { text: string; packets?: number; bytes?: number }
export interface FwChain { name: 'input' | 'forward' | 'output'; policy: 'accept' | 'drop'; rules: FwRule[] }
export interface FirewallDraft { chains: { name: FwChain['name']; policy: FwChain['policy']; rules: { text: string }[] }[] }
export interface FirewallInfo {
  available: boolean; ufw_active: boolean;
  chains: FwChain[]; ruleset: string; other_tables: string[];
  pending: PendingChange | null; draft: FirewallDraft | null;
}

// ---- docker (stage 5) ----
export interface DockerContainer {
  id: string; name: string; image: string; state: string; status: string; ports: string[];
  created: number; cpu: number; mem_used: number; mem_limit: number;
}
export interface DockerImage { id: string; repo_tags: string[]; size: number; created: number; used: boolean }
export interface DockerInfo { available: boolean; containers: DockerContainer[]; images: DockerImage[] }

// ---- updates and audit (stage 6) ----
export interface UpdateStep { id: 'verify' | 'backup' | 'stop' | 'replace' | 'migrate' | 'start'; state: 'pending' | 'running' | 'done' | 'failed' }
export interface ServerUpdateRun {
  run_id: string; state: 'idle' | 'running' | 'done' | 'failed' | 'rolled_back';
  steps: UpdateStep[]; from: string; to: string; error?: string; started_at: number; finished_at: number;
}
export interface AgentUpdateRow {
  id: string; name: string; os: string; address: string; version: string;
  status: 'current' | 'available' | 'unsupported' | 'updating' | 'queued' | 'rollback' | 'failed';
  online: boolean; target?: string;
  step?: number; steps_total: number; step_id?: 'download' | 'verify' | 'backup' | 'replace' | 'restart';
  message?: string;
}
export interface UpdateHistoryRow { id: number; ts: number; kind: 'server' | 'agents'; from: string; to: string; status: 'success' | 'failed' | 'rolled_back'; detail: string }
export type RolloutMode = 'all' | 'one_by_one' | 'first_then_rest';
export interface UpdatesInfo {
  channel: 'stable' | 'beta';
  server: {
    current: string; available: string | null; checked_at: number; check_error: string;
    notes: string[]; db_migrations: { applied: number; shipped: number; pending: number };
    last_backup: number; source: string; can_update: boolean; update_run: ServerUpdateRun;
  };
  cache: { version: string; size: number; downloaded_at: number; verified: boolean; downloaded: boolean }[];
  agents: AgentUpdateRow[];
  rollout: { state: 'running' | 'done' | 'stopped'; mode: RolloutMode; stop_on_error: boolean; version: string } | null;
  history: UpdateHistoryRow[];
  min_agent: string; agents_target: string; latest_known: string | null;
}
export interface AuditItem {
  id: number; ts: number; actor: string; action: string; target_type: string; target_name: string;
  result: string; client_ip: string; remote_addr: string; details: Record<string, unknown> | null;
}
export interface AuditPage { items: AuditItem[]; next_before: number }

export interface ServerDetail {
  modules: string[]; // enabled agent modules; empty = unknown (old agent)
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

  services: (id: string) => request<{ services: ServiceUnit[] }>('GET', `api/servers/${enc(id)}/services`),
  /** Rejects with ApiError status 409 and `dangerous` when the action needs `confirm: true`. */
  serviceAction: (id: string, unit: string, action: ServiceAction, confirm: boolean) =>
    request<{ ok: boolean }>('POST', `api/servers/${enc(id)}/services/${enc(unit)}/${action}`, { confirm }),
  apt: (id: string) => request<AptList>('GET', `api/servers/${enc(id)}/apt`),
  reboot: (id: string) => request<{ ok: boolean }>('POST', `api/servers/${enc(id)}/reboot`, { confirm: true }),
  processes: (id: string, sort: 'cpu' | 'mem', n = 10) =>
    request<ProcessList>('GET', `api/servers/${enc(id)}/processes?sort=${sort}&n=${n}`),
  killProcess: (id: string, pid: number, signal: 'TERM' | 'KILL') =>
    request<{ ok: boolean }>('POST', `api/servers/${enc(id)}/processes/${pid}/kill`, { signal, confirm: true }),
  // files
  files: (id: string, path: string) => request<FileList>('GET', `api/servers/${enc(id)}/files?path=${enc(path)}`),
  fileContent: (id: string, path: string) => request<FileContent>('GET', `api/servers/${enc(id)}/files/content?path=${enc(path)}`),
  /** Pass the mtime you read; a 409 means the file changed meanwhile. */
  fileSave: (id: string, path: string, content: string, mtime?: number) =>
    request<{ ok: boolean; mtime: number }>('PUT', `api/servers/${enc(id)}/files/content`, { path, content, mtime }),
  fileMkdir: (id: string, path: string) => request<{ ok: boolean }>('POST', `api/servers/${enc(id)}/files/mkdir`, { path }),
  fileCreate: (id: string, path: string) => request<{ ok: boolean }>('POST', `api/servers/${enc(id)}/files/create`, { path }),
  fileDelete: (id: string, path: string) =>
    request<{ ok: boolean }>('POST', `api/servers/${enc(id)}/files/delete`, { path, confirm: true }),
  /** Relative URL for a download link. */
  fileDownloadUrl: (id: string, path: string) => `api/servers/${enc(id)}/files/download?path=${enc(path)}`,
  uploadFile: (id: string, path: string, file: File, onProgress?: (fraction: number) => void) =>
    new Promise<void>((resolve, reject) => {
      const xhr = new XMLHttpRequest();
      xhr.open('POST', `api/servers/${enc(id)}/files/upload?path=${enc(path)}`);
      xhr.setRequestHeader('X-CSRF-Token', csrfToken);
      xhr.withCredentials = true;
      xhr.upload.onprogress = (e) => e.lengthComputable && onProgress?.(e.loaded / e.total);
      xhr.onload = () => {
        if (xhr.status >= 200 && xhr.status < 300) return resolve();
        let msg: string | undefined;
        try { msg = (JSON.parse(xhr.responseText) as { error?: string }).error; } catch { /* no body */ }
        reject(new ApiError(xhr.status, msg));
      };
      xhr.onerror = () => reject(new ApiError(0));
      xhr.send(file);
    }),

  // network
  network: (id: string) => request<NetworkInfo>('GET', `api/servers/${enc(id)}/network`),
  networkValidate: (id: string, draft: NetworkDraft) =>
    request<{ errors: FieldError[] }>('POST', `api/servers/${enc(id)}/network/validate`, draft),
  /** 422 with errors when the draft is invalid. */
  networkSaveDraft: (id: string, draft: NetworkDraft) =>
    request<{ ok: boolean }>('PUT', `api/servers/${enc(id)}/network/draft`, draft),
  networkResetDraft: (id: string) => request<{ ok: boolean }>('DELETE', `api/servers/${enc(id)}/network/draft`),
  /** Without confirm, answers 409 {warnings} when the change is risky; repeat with confirm=true to apply. */
  networkApply: (id: string, confirm: boolean) =>
    request<PendingChange>('POST', `api/servers/${enc(id)}/network/apply`, { confirm }),
  networkConfirm: (id: string, changeId: string) =>
    request<{ ok: boolean }>('POST', `api/servers/${enc(id)}/network/confirm`, { id: changeId }),
  networkRollback: (id: string, changeId: string) =>
    request<{ ok: boolean }>('POST', `api/servers/${enc(id)}/network/rollback`, { id: changeId }),

  // firewall
  firewall: (id: string) => request<FirewallInfo>('GET', `api/servers/${enc(id)}/firewall`),
  firewallValidate: (id: string, draft: FirewallDraft) =>
    request<{ ok: boolean; error?: string }>('POST', `api/servers/${enc(id)}/firewall/validate`, draft),
  firewallSaveDraft: (id: string, draft: FirewallDraft) =>
    request<{ ok: boolean }>('PUT', `api/servers/${enc(id)}/firewall/draft`, draft),
  firewallResetDraft: (id: string) => request<{ ok: boolean }>('DELETE', `api/servers/${enc(id)}/firewall/draft`),
  firewallApply: (id: string, confirm: boolean) =>
    request<PendingChange>('POST', `api/servers/${enc(id)}/firewall/apply`, { confirm }),
  firewallConfirm: (id: string, changeId: string) =>
    request<{ ok: boolean }>('POST', `api/servers/${enc(id)}/firewall/confirm`, { id: changeId }),
  firewallRollback: (id: string, changeId: string) =>
    request<{ ok: boolean }>('POST', `api/servers/${enc(id)}/firewall/rollback`, { id: changeId }),

  // docker
  docker: (id: string) => request<DockerInfo>('GET', `api/servers/${enc(id)}/docker`),
  dockerContainerAction: (id: string, cid: string, action: 'start' | 'stop' | 'restart' | 'remove') =>
    request<{ ok: boolean }>('POST', `api/servers/${enc(id)}/docker/containers/${enc(cid)}/${action}`, { confirm: action === 'remove' }),
  dockerRemoveImage: (id: string, imageId: string) =>
    request<{ ok: boolean }>('DELETE', `api/servers/${enc(id)}/docker/images/${enc(imageId)}`),
  // updates (stage 6)
  updates: () => request<UpdatesInfo>('GET', 'api/updates'),
  updatesSummary: () => request<{ count: number }>('GET', 'api/updates/summary'),
  updatesCheck: () => request<UpdatesInfo>('POST', 'api/updates/check'),
  updatesChannel: (channel: 'stable' | 'beta') => request<UpdatesInfo>('PUT', 'api/updates/channel', { channel }),
  /** Download and verify a release into the server cache ahead of time. */
  updatesPrefetch: (version: string) => request<UpdatesInfo>('POST', 'api/updates/cache', { version }),
  /** Starts the server update (backup, replace, migrate, restart); the panel is unreachable for a short time. */
  updatesServer: (version: string) => request<{ ok: boolean }>('POST', 'api/updates/server', { version, confirm: true }),
  updatesAgents: (serverIds: string[] | 'all', mode: RolloutMode, stopOnError: boolean) =>
    request<UpdatesInfo>('POST', 'api/updates/agents', serverIds === 'all'
      ? { all: true, mode, stop_on_error: stopOnError }
      : { server_ids: serverIds, mode, stop_on_error: stopOnError }),

  // audit log
  audit: (params: { before?: number; limit?: number; action?: string; actor?: string; result?: string; q?: string } = {}) => {
    const q = new URLSearchParams();
    for (const [k, v] of Object.entries(params)) if (v !== undefined && v !== '' && v !== 0) q.set(k, String(v));
    return request<AuditPage>('GET', `api/audit?${q.toString()}`);
  },
};

/**
 * Opens a WebSocket stream to the agent. Server -> client: binary frames are output bytes
 * (terminal: raw; logs/apt: UTF-8 text); text frames are JSON `{closed: string}` or `{error: string}`.
 * Client -> server (terminal): binary frames are keystrokes, text frame `{"resize":{"cols":N,"rows":N}}`.
 * Closing the socket ends the stream. params: terminal cols,rows; logs unit,priority(0-7),kernel(0|1),lines;
 * apt mode=update|upgrade|security.
 */
export function openStream(id: string, kind: StreamKind, params: Record<string, string | number> = {}): WebSocket {
  const url = new URL(`api/servers/${enc(id)}/stream`, location.href);
  url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:';
  url.searchParams.set('kind', kind);
  for (const [k, v] of Object.entries(params)) url.searchParams.set(k, String(v));
  const ws = new WebSocket(url);
  ws.binaryType = 'arraybuffer';
  return ws;
}