import { writable, get } from 'svelte/store';
import { openStream } from '../../../api';
import { t } from '../../../i18n';
import { showToast } from '../../toast';
import { applyPkgEvents, createAptParser, lastProgress, type AptProgress, type RunPkg } from './apt-events';

export type AptMode = 'update' | 'security' | 'upgrade';

export interface AptRun {
  id: number;
  serverId: string;
  mode: AptMode;
  text: string;
  active: boolean;
  ok: boolean;
  /** Packages of an install run with their statuses, in queue order. */
  pkgs: RunPkg[];
  /** Overall progress of the current phase, null until apt reports it. */
  progress: AptProgress | null;
  /** Repositories apt has already asked during a check. */
  sources: number;
}

/** The current or last apt run. Lives outside the tab so it survives switching tabs. */
export const aptRun = writable<AptRun | null>(null);

let seq = 0;

/** Starts an apt stream unless a run is already active. Output is appended to aptRun. */
export function startAptRun(serverId: string, mode: AptMode): void {
  if (get(aptRun)?.active) return;

  const id = ++seq;
  const decoder = new TextDecoder('utf-8');
  const parser = createAptParser();
  let done = false;
  let failed = false;

  const patch = (change: (run: AptRun) => AptRun) =>
    aptRun.update((run) => (run && run.id === id ? change(run) : run));
  const append = (chunk: string) => {
    if (chunk) patch((run) => ({ ...run, text: run.text + chunk }));
  };
  // Stream bytes: the agent mixes event lines into the console text.
  const feed = (chunk: string) => {
    if (!chunk) return;
    const res = parser(chunk);
    patch((run) => ({
      ...run,
      text: run.text + res.text,
      pkgs: res.events.length ? applyPkgEvents(run.pkgs, res.events) : run.pkgs,
      progress: lastProgress(run.progress, res.events),
      sources: run.sources + res.sources,
    }));
  };
  const finish = () => {
    if (done) return;
    done = true;
    feed(decoder.decode());
    patch((run) => ({ ...run, active: false, ok: !failed }));
    showToast(failed ? t('apt.failed') : mode === 'update' ? t('apt.checkDone') : t('apt.done'));
  };

  aptRun.set({ id, serverId, mode, text: '', active: true, ok: true, pkgs: [], progress: null, sources: 0 });

  const ws = openStream(serverId, 'apt', { mode });
  ws.onmessage = (event: MessageEvent) => {
    if (typeof event.data === 'string') {
      let frame: { closed?: unknown; error?: unknown } = {};
      try {
        frame = JSON.parse(event.data) as typeof frame;
      } catch {
        append(event.data);
        return;
      }
      if (frame.error !== undefined) {
        failed = true;
        append(`\n${String(frame.error)}\n`);
      }
      if (frame.closed !== undefined) {
        append(`\n[${String(frame.closed)}]\n`);
        finish();
        ws.close();
      }
      return;
    }
    feed(decoder.decode(new Uint8Array(event.data as ArrayBuffer), { stream: true }));
  };
  ws.onerror = () => {
    failed = true;
    append(`\n${t('common.networkError')}\n`);
  };
  ws.onclose = finish;
}
