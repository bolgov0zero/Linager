import type { SSHGate, SSHState, SSHStep } from '../../api';
import type { Locale } from '../../i18n';
import type { Stage } from '../ui/StageList.svelte';
import { fmtBytes, type Tr } from '../updates/format';

/** The eight checks come first; whatever else the server lists belongs to the install itself. */
export const CHECK_IDS = ['connect', 'hostkey', 'auth', 'system', 'disk', 'rights', 'reach', 'agent'];

/** Loopback, "any" and link-local (cloud metadata) addresses are refused by the server; the form says so at once. */
export function isBlockedHost(host: string): boolean {
  return /^(127\.|169\.254\.|localhost$|0\.0\.0\.0$|\[?::1\]?$)/i.test(host.trim());
}

export function isCheckStep(step: SSHStep): boolean {
  return CHECK_IDS.includes(step.id);
}

/** A string out of the loosely typed data of a gate. */
export function gateStr(gate: SSHGate | undefined, key: string): string {
  const v = gate?.data?.[key];
  return typeof v === 'string' || typeof v === 'number' ? String(v) : '';
}

export function gateList(gate: SSHGate | undefined, key: string): string[] {
  const v = gate?.data?.[key];
  return Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string') : [];
}

/** "1 строка / 2 строки / 5 строк" (and one/many for English). */
export function pluralKey(n: number, locale: Locale): 'one' | 'few' | 'many' {
  if (locale === 'en') return n === 1 ? 'one' : 'many';
  const m10 = n % 10;
  const m100 = n % 100;
  if (m10 === 1 && m100 !== 11) return 'one';
  if (m10 >= 2 && m10 <= 4 && (m100 < 10 || m100 >= 20)) return 'few';
  return 'many';
}

/** The grey line under a stage, made of the raw fact the server reports in `detail`. */
function stageNote(step: SSHStep, state: SSHState, tr: Tr): string {
  const detail = step.detail ?? '';
  if (step.state === 'failed') return detail;
  switch (step.id) {
    case 'hostkey':
      return step.state === 'running' && state.gate?.kind === 'hostkey' ? tr('asv.ssh.note.hostkeyAsk') : detail;
    case 'auth':
      if (detail === 'password') return tr('asv.ssh.note.authPassword');
      if (detail === 'key') return tr('asv.ssh.note.authKey');
      return detail;
    case 'rights':
      if (step.state === 'running' && state.gate?.kind === 'sudo') return tr('asv.ssh.note.sudoAsk');
      if (detail === 'root') return tr('asv.ssh.note.rootLogin');
      if (detail === 'sudo') {
        if (state.mode === 'nopass') return tr('asv.ssh.note.sudoNopass');
        if (state.mode === 'password') return tr('asv.ssh.note.sudoPassword');
        return tr('asv.ssh.note.sudo');
      }
      return detail;
    case 'reach':
      return step.state === 'done' && detail ? tr('asv.ssh.note.reachOk', { addr: detail }) : detail;
    case 'agent':
      if (step.state === 'warn') {
        return detail ? tr('asv.ssh.note.agentExists', { version: detail }) : tr('asv.ssh.note.agentExistsNoVer');
      }
      if (step.state === 'done') {
        return detail ? tr('asv.ssh.note.agentReinstall', { version: detail }) : tr('asv.ssh.note.agentNone');
      }
      return '';
    case 'download':
      if (step.size && step.size > 0) {
        return step.state === 'running'
          ? tr('asv.ssh.note.of', { done: fmtBytes(step.bytes ?? 0, tr), total: fmtBytes(step.size, tr) })
          : fmtBytes(step.size, tr);
      }
      return detail;
    case 'verify':
      return detail ? tr('asv.ssh.note.hash', { hash: detail }) : '';
    case 'bind':
    case 'online':
      return detail ? tr('asv.ssh.note.server', { name: detail }) : '';
    default:
      return detail;
  }
}

/** The rows of the list of stages: the checks while the checks run, the install steps once it has started. */
export function sshStages(state: SSHState, installing: boolean, tr: Tr): Stage[] {
  return state.steps
    .filter((s) => (installing ? !isCheckStep(s) : isCheckStep(s)))
    .map((s) => ({
      id: s.id,
      state: s.state,
      name: tr(`asv.ssh.step.${s.id}`),
      note: stageNote(s, state, tr),
      startedMs: s.started_ms,
      finishedMs: s.finished_ms,
      progress: s.id === 'download' && s.size && s.size > 0 && (s.state === 'running' || s.state === 'done') ? Math.min((s.state === 'done' ? s.size : (s.bytes ?? 0)) / s.size, 1) : undefined,
    }));
}

/** The code that explains a failure: the one of the step that failed, else the one of the session. */
export function failureCode(state: SSHState): { code: string; detail: string } {
  const step = state.steps.find((s) => s.state === 'failed' && s.id !== 'rollback');
  return { code: step?.code || state.error || '', detail: step?.detail ?? '' };
}

/** The plain-language explanation of a failure code; an unknown code is shown as it is. */
export function failureText(code: string, auth: 'password' | 'key', vars: Record<string, string | number>, tr: Tr): string {
  if (!code) return '';
  const key = `asv.ssh.err.${code === 'auth_failed' && auth === 'key' ? 'auth_failed_key' : code}`;
  const text = tr(key, vars);
  return text === key ? tr('asv.ssh.err.unknown', { code }) : text;
}
