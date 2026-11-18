import type { ServerCard } from '../../api';

/** ok = normal, warn = yellow, crit = red, off = offline (grey). */
export type Level = 'ok' | 'warn' | 'crit' | 'off';

export interface Tag {
  id: 'reboot' | 'updates' | 'images' | 'failed' | 'offline' | 'agentUpdate' | 'agentOld' | 'clean';
  n?: number;
  tone: 'ok' | 'warn' | 'crit' | 'muted' | 'plain';
}

export interface Totals {
  total: number;
  online: number;
  offline: number;
  /** Servers that need attention: warning, critical or offline. */
  problems: number;
  critical: number;
  /** Sum of apt upgrades over all servers with a known count. */
  updates: number;
  /** Sum of Docker containers with a newer image over all servers with a known count. */
  imageUpdates: number;
  rebootPending: number;
  /** Average CPU over online servers, null when none. */
  cpuAvg: number | null;
}

export function levelOf(card: ServerCard): Level {
  if (!card.online) return 'off';
  const s = card.sample;
  if (!s) return 'ok';
  if (s.cpu > 90 || s.mem_pct > 92 || s.disk_pct > 90) return 'crit';
  if (
    s.cpu > 75 ||
    s.mem_pct > 85 ||
    s.disk_pct > 80 ||
    s.failed_services > 0 ||
    s.reboot_required ||
    s.updates >= 10
  ) {
    return 'warn';
  }
  return 'ok';
}

/** Tone for a single percentage value shown as text. */
export function toneOfPct(value: number | null): 'crit' | 'warn' | 'plain' {
  if (value === null) return 'plain';
  if (value > 90) return 'crit';
  if (value > 75) return 'warn';
  return 'plain';
}

/** Agent update state taken from the updates API: an update is available, or the version is unsupported. */
export type AgentFlag = 'available' | 'unsupported' | null;

function agentTag(agent: AgentFlag): Tag | null {
  if (agent === 'unsupported') return { id: 'agentOld', tone: 'warn' };
  if (agent === 'available') return { id: 'agentUpdate', tone: 'plain' };
  return null;
}

/** Containers with a newer image: plain, warn from 5. Nothing for 0, -1 (unknown) or an agent without the field. */
export function imagesTag(count: number | undefined): Tag | null {
  if (count === undefined || count <= 0) return null;
  return { id: 'images', n: count, tone: count >= 5 ? 'warn' : 'plain' };
}

export function tagsOf(card: ServerCard, agent: AgentFlag = null): Tag[] {
  const s = card.sample;
  if (!card.online) {
    const tags: Tag[] = [{ id: 'offline', tone: 'muted' }];
    const updates = s?.updates ?? -1;
    if (updates > 0) tags.push({ id: 'updates', n: updates, tone: updates >= 10 ? 'warn' : 'plain' });
    const offlineImages = imagesTag(s?.image_updates);
    if (offlineImages) tags.push(offlineImages);
    const offlineAgent = agentTag(agent);
    if (offlineAgent) tags.push(offlineAgent);
    return tags;
  }
  const tags: Tag[] = [];
  if (s) {
    if (s.reboot_required) tags.push({ id: 'reboot', tone: 'warn' });
    if (s.updates > 0) tags.push({ id: 'updates', n: s.updates, tone: s.updates >= 10 ? 'warn' : 'plain' });
    const images = imagesTag(s.image_updates);
    if (images) tags.push(images);
    if (s.failed_services > 0) tags.push({ id: 'failed', n: s.failed_services, tone: 'crit' });
  }
  const agentFlag = agentTag(agent);
  if (agentFlag) tags.push(agentFlag);
  if (tags.length === 0) tags.push({ id: 'clean', tone: 'ok' });
  return tags;
}

export function summarize(cards: ServerCard[]): Totals {
  let online = 0;
  let problems = 0;
  let critical = 0;
  let updates = 0;
  let imageUpdates = 0;
  let rebootPending = 0;
  let cpuSum = 0;
  let cpuCount = 0;
  for (const card of cards) {
    const level = levelOf(card);
    if (card.online) online++;
    if (level !== 'ok') problems++;
    if (level === 'crit') critical++;
    const s = card.sample;
    if (!s) continue;
    if (s.updates > 0) updates += s.updates;
    if ((s.image_updates ?? 0) > 0) imageUpdates += s.image_updates ?? 0;
    if (s.reboot_required) rebootPending++;
    if (card.online) {
      cpuSum += s.cpu;
      cpuCount++;
    }
  }
  return {
    total: cards.length,
    online,
    offline: cards.length - online,
    problems,
    critical,
    updates,
    imageUpdates,
    rebootPending,
    cpuAvg: cpuCount > 0 ? cpuSum / cpuCount : null,
  };
}

/** The kind of host an agent runs on, from the OS name it reports (a Windows name starts with "Windows"). */
export function osKind(os: string): 'windows' | 'linux' {
  return os.startsWith('Windows') ? 'windows' : 'linux';
}
