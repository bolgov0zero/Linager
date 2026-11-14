import { api, ApiError, type ApplyWarning, type PendingChange } from '../../../api';

export type ApplyResult =
  | { kind: 'applied'; pending: PendingChange }
  | { kind: 'warnings'; warnings: ApplyWarning[] };

function readWarnings(data: unknown): ApplyWarning[] | null {
  if (!data || typeof data !== 'object' || !('warnings' in data)) return null;
  const list = (data as { warnings: unknown }).warnings;
  if (!Array.isArray(list)) return null;
  const valid = list.filter(
    (w): w is ApplyWarning => !!w && typeof w === 'object' && typeof (w as ApplyWarning).message === 'string',
  );
  return valid.length > 0 ? valid : null;
}

function readError(data: unknown): string | undefined {
  if (data && typeof data === 'object' && 'error' in data && typeof data.error === 'string') return data.error;
  return undefined;
}

/**
 * Calls the apply endpoint directly so the 409 body (warnings) can be read.
 * Without confirm the server answers 409 for risky changes; repeat with confirm=true to apply.
 */
export async function applyNetwork(id: string, confirm: boolean): Promise<ApplyResult> {
  // api.me() refreshes the CSRF token kept inside api.ts.
  const session = await api.me();

  let res: Response;
  try {
    res = await fetch(`api/servers/${encodeURIComponent(id)}/network/apply`, {
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
        'X-CSRF-Token': session.csrf,
      },
      credentials: 'same-origin',
      body: JSON.stringify({ confirm }),
    });
  } catch {
    throw new ApiError(0);
  }

  const text = await res.text();
  let data: unknown = null;
  try {
    data = text ? JSON.parse(text) : null;
  } catch {
    data = null;
  }

  if (res.status === 409) {
    const warnings = readWarnings(data);
    if (warnings) return { kind: 'warnings', warnings };
  }
  if (!res.ok) throw new ApiError(res.status, readError(data));
  return { kind: 'applied', pending: data as PendingChange };
}
