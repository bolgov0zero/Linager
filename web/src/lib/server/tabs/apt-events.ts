/** Events the agent mixes into the apt stream (see agent/mod_apt_events.go): MARK + JSON + "\n". */
export const APT_MARK = '\x1e';

export type PkgState = 'queued' | 'running' | 'done' | 'error' | 'skipped';

export interface RunPkg {
  name: string;
  current: string;
  candidate: string;
  security: boolean;
  reboot: boolean;
  state: PkgState;
  msg: string;
}

export interface AptProgress {
  stage: 'download' | 'install';
  pct: number;
  cur: number;
  total: number;
}

export type AptEvent =
  | {
      t: 'queue';
      pkgs: { name: string; current?: string; candidate?: string; security?: boolean; reboot?: boolean }[];
    }
  | { t: 'pkg'; name: string; state: PkgState; msg?: string }
  | { t: 'progress'; stage: 'download' | 'install'; pct: number; cur?: number; total?: number };

export interface FeedResult {
  /** Console text with the event lines cut out. */
  text: string;
  events: AptEvent[];
  /** Complete lines "Hit:3 ...", "Get:4 ..." seen in this chunk: the repositories apt has asked. */
  sources: number;
}

const sourceRe = /^(Hit|Get|Ign|Err):\d+ /;

/**
 * Splits the decoded stream into console text and events. An event line may arrive in several
 * chunks and may start in the middle of a text line, so the unfinished part is kept between calls.
 */
export function createAptParser(): (chunk: string) => FeedResult {
  let pending = ''; // an event line that has not ended yet
  let tail = ''; // the unfinished last text line, for counting sources

  return (chunk: string): FeedResult => {
    let data = pending + chunk;
    pending = '';
    let text = '';
    const events: AptEvent[] = [];
    for (;;) {
      const i = data.indexOf(APT_MARK);
      if (i < 0) {
        text += data;
        break;
      }
      text += data.slice(0, i);
      const j = data.indexOf('\n', i + 1);
      if (j < 0) {
        pending = data.slice(i);
        break;
      }
      try {
        const ev = JSON.parse(data.slice(i + 1, j)) as AptEvent;
        if (ev && typeof ev.t === 'string') events.push(ev);
      } catch {
        // a damaged event line is dropped, the console text is unaffected
      }
      data = data.slice(j + 1);
    }

    let sources = 0;
    const lines = (tail + text).split('\n');
    tail = lines.pop() ?? '';
    for (const line of lines) if (sourceRe.test(line)) sources++;
    return { text, events, sources };
  };
}

/** Applies events to the package list; unknown packages are added to the end of the queue. */
export function applyPkgEvents(pkgs: RunPkg[], events: AptEvent[]): RunPkg[] {
  let list = pkgs;
  let copied = false;
  const own = () => {
    if (!copied) {
      list = list.slice();
      copied = true;
    }
  };
  for (const ev of events) {
    if (ev.t === 'queue') {
      for (const p of ev.pkgs) {
        if (list.some((x) => x.name === p.name)) continue;
        own();
        list.push({
          name: p.name,
          current: p.current ?? '',
          candidate: p.candidate ?? '',
          security: !!p.security,
          reboot: !!p.reboot,
          state: 'queued',
          msg: '',
        });
      }
    } else if (ev.t === 'pkg') {
      const idx = list.findIndex((x) => x.name === ev.name);
      if (idx < 0) continue;
      own();
      list[idx] = { ...list[idx], state: ev.state, msg: ev.msg ?? list[idx].msg };
    }
  }
  return list;
}

/** The latest progress event of the batch, or the previous progress. */
export function lastProgress(prev: AptProgress | null, events: AptEvent[]): AptProgress | null {
  let out = prev;
  for (const ev of events) {
    if (ev.t === 'progress') out = { stage: ev.stage, pct: ev.pct, cur: ev.cur ?? 0, total: ev.total ?? 0 };
  }
  return out;
}
