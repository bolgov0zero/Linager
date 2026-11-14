import { api, ApiError, type ApplyWarning, type PendingChange } from '../../../api';

export type ApplyOutcome =
  | { kind: 'applied'; pending: PendingChange }
  | { kind: 'warnings'; warnings: ApplyWarning[] };

// Session CSRF token, fetched once through api.me(). Dropped on 403 so the next call refetches it.
let csrf: string | null = null;

async function csrfToken(): Promise<string> {
  if (csrf === null) csrf = (await api.me()).csrf;
  return csrf;
}

function isWarningBody(body: unknown): body is { warnings: ApplyWarning[] } {
  if (typeof body !== 'object' || body === null) return false;
  const list = (body as { warnings?: unknown }).warnings;
  return Array.isArray(list) && list.length > 0;
}

function errorMessage(body: unknown): string | undefined {
  if (typeof body !== 'object' || body === null) return undefined;
  const message = (body as { error?: unknown }).error;
  return typeof message === 'string' ? message : undefined;
}

/**
 * POST firewall/apply. api.firewallApply hides the 409 body, so the request is sent here.
 * Resolves with the warnings on 409 (repeat with confirm=true to apply anyway).
 */
export async function applyFirewall(serverId: string, confirm: boolean): Promise<ApplyOutcome> {
  let res: Response;
  try {
    res = await fetch(`api/servers/${encodeURIComponent(serverId)}/firewall/apply`, {
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
        'X-CSRF-Token': await csrfToken(),
      },
      credentials: 'same-origin',
      body: JSON.stringify({ confirm }),
    });
  } catch {
    throw new ApiError(0);
  }

  const text = await res.text();
  let body: unknown = null;
  try {
    body = text ? JSON.parse(text) : null;
  } catch {
    body = null;
  }

  if (res.ok) return { kind: 'applied', pending: body as PendingChange };
  if (res.status === 403) csrf = null;
  if (res.status === 409 && isWarningBody(body)) return { kind: 'warnings', warnings: body.warnings };
  throw new ApiError(res.status, errorMessage(body));
}
