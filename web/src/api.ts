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
};
