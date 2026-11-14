import { writable, get } from 'svelte/store';
import { openStream } from '../../../api';
import { t } from '../../../i18n';
import { showToast } from '../../toast';

export type AptMode = 'update' | 'security' | 'upgrade';

export interface AptRun {
  id: number;
  serverId: string;
  mode: AptMode;
  text: string;
  active: boolean;
  ok: boolean;
}

/** The current or last apt run. Lives outside the tab so it survives switching tabs. */
export const aptRun = writable<AptRun | null>(null);

let seq = 0;

/** Starts an apt stream unless a run is already active. Output is appended to aptRun. */
export function startAptRun(serverId: string, mode: AptMode): void {
  if (get(aptRun)?.active) return;

  const id = ++seq;
  const decoder = new TextDecoder('utf-8');
  let done = false;
  let failed = false;

  const patch = (change: (run: AptRun) => AptRun) =>
    aptRun.update((run) => (run && run.id === id ? change(run) : run));
  const append = (chunk: string) => {
    if (chunk) patch((run) => ({ ...run, text: run.text + chunk }));
  };
  const finish = () => {
    if (done) return;
    done = true;
    append(decoder.decode());
    patch((run) => ({ ...run, active: false, ok: !failed }));
    showToast(failed ? t('apt.failed') : mode === 'update' ? t('apt.checkDone') : t('apt.done'));
  };

  aptRun.set({ id, serverId, mode, text: '', active: true, ok: true });

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
    append(decoder.decode(new Uint8Array(event.data as ArrayBuffer), { stream: true }));
  };
  ws.onerror = () => {
    failed = true;
    append(`\n${t('common.networkError')}\n`);
  };
  ws.onclose = finish;
}
