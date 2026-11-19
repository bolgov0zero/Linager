<script lang="ts">
  import { api, ApiError, type ProcessInfo } from '../../api';
  import { tr } from '../../i18n';
  import { formatBytes } from '../server/format';
  import { showToast } from '../toast';
  import { heat, nextSort, procName, sortProcs, type ProcKey, type SortState } from './dispatcher';

  /** The "Processes" tab of the Dispatcher: the list as the Task Manager shows it, sorted by a column, and the two ways to end a process. */
  let { serverId, query, active, windows }: { serverId: string; query: string; active: boolean; /** Windows ends a process or its tree, Linux sends SIGTERM or SIGKILL: the texts differ. */ windows: boolean } = $props();

  const REFRESH_MS = 3000;
  const ALL = 2000; // the agent's cap

  let rows = $state<ProcessInfo[]>([]);
  let loaded = $state(false);
  let problem = $state<'offline' | 'disabled' | 'error' | null>(null);
  let sort = $state<SortState<ProcKey>>({ key: 'cpu', dir: 'desc' });
  let selected = $state<number | null>(null);
  let ask = $state<{ pid: number; signal: 'TERM' | 'KILL' } | null>(null);
  let busy = $state(false);
  let note = $state('');

  let refreshNow: () => void = () => {};

  // The list is read while the tab is in front (a minimized window stops asking the computer).
  $effect(() => {
    const id = serverId;
    if (!active) return;
    let alive = true;
    let inFlight = false;
    const run = async (): Promise<void> => {
      if (inFlight) return;
      inFlight = true;
      try {
        const list = await api.processes(id, 'cpu', ALL);
        if (!alive) return;
        rows = list.processes;
        problem = null;
      } catch (e) {
        if (!alive) return;
        problem = e instanceof ApiError && e.status === 503 ? 'offline' : e instanceof ApiError && e.status === 501 ? 'disabled' : 'error';
      } finally {
        inFlight = false;
        if (alive) loaded = true;
      }
    };
    refreshNow = () => void run();
    void run();
    const timer = setInterval(() => void run(), REFRESH_MS);
    return () => {
      alive = false;
      clearInterval(timer);
    };
  });

  const q = $derived(query.trim().toLowerCase());
  const shown = $derived(
    sortProcs(
      rows.filter((p) => !q || procName(p.cmd).toLowerCase().includes(q) || String(p.pid).includes(q) || p.user.toLowerCase().includes(q)),
      sort,
    ),
  );
  const current = $derived(selected === null ? undefined : rows.find((p) => p.pid === selected));
  // a process that has ended is no longer picked
  $effect(() => {
    if (loaded && selected !== null && !rows.some((p) => p.pid === selected)) selected = null;
  });

  const cpuSum = $derived(rows.reduce((n, p) => n + p.cpu, 0));
  const ramSum = $derived(rows.reduce((n, p) => n + p.rss, 0));
  const num = (n: number): string => n.toFixed(1).replace('.', ',');

  const cols: { key: ProcKey; text: boolean }[] = [
    { key: 'name', text: true },
    { key: 'pid', text: false },
    { key: 'user', text: true },
    { key: 'cpu', text: false },
    { key: 'ram', text: false },
  ];
  const colLabel = (k: ProcKey): string => $tr(`disp.col.${k}`);
  const colTotal = (k: ProcKey): string => (k === 'cpu' ? `${Math.round(cpuSum)}%` : k === 'ram' ? formatBytes(ramSum) : '');

  /** A cell is shaded the way the Task Manager does it: the busier, the warmer. */
  function warm(share: number, on: boolean): string {
    return on || share <= 0 ? '' : `background:color-mix(in srgb, var(--warn) ${Math.round(8 + share * 42)}%, transparent)`;
  }

  function pickCol(k: ProcKey, text: boolean): void {
    sort = nextSort(sort, k, text);
  }

  const agentNote = (p: ProcessInfo): string => (['linager', 'linager.exe'].includes(procName(p.cmd).toLowerCase()) ? ` ${$tr('disp.agentNote')}` : '');
  const askText = (p: ProcessInfo, signal: 'TERM' | 'KILL'): string =>
    $tr(`disp.${signal === 'KILL' ? 'killText' : 'termText'}${windows ? '' : 'Linux'}`, { name: procName(p.cmd), pid: p.pid }) + agentNote(p);

  async function run(): Promise<void> {
    const a = ask;
    const p = rows.find((x) => x.pid === a?.pid);
    if (!a || !p || busy) return;
    busy = true;
    try {
      await api.killProcess(serverId, a.pid, a.signal);
      note = windows ? $tr(a.signal === 'KILL' ? 'disp.doneKill' : 'disp.done', { name: procName(p.cmd) }) : $tr('proc.sent', { pid: a.pid });
      selected = null;
      refreshNow();
    } catch {
      showToast($tr('proc.killFailed', { pid: a.pid }));
    } finally {
      busy = false;
      ask = null;
    }
  }
</script>

<div class="dsp-pane">
  {#if problem === 'offline' || problem === 'disabled'}
    <p class="dsp-empty">{$tr(problem === 'offline' ? 'svc.offline' : 'svc.disabled')}</p>
  {:else if problem === 'error' && !loaded}
    <p class="dsp-empty">{$tr('common.genericError')}</p>
  {:else if !loaded}
    <p class="dsp-empty">{$tr('common.loading')}</p>
  {:else}
    <div class="dsp-list">
      <div class="dsp-lh dsp-p">
        {#each cols as c (c.key)}
          <button
            class="dsp-th"
            class:num={!c.text}
            class:on={sort.key === c.key}
            type="button"
            title={c.key === 'cpu' ? $tr('disp.cpuHint') : $tr('disp.sortHint')}
            onclick={() => pickCol(c.key, c.text)}
          >
            <span class="dsp-tot">{colTotal(c.key)}</span>
            <span class="dsp-lab">{colLabel(c.key)}<i class="dsp-arr" class:up={sort.key === c.key && sort.dir === 'asc'} class:down={sort.key === c.key && sort.dir === 'desc'} class:none={sort.key !== c.key}></i></span>
          </button>
        {/each}
      </div>
      <div class="dsp-scroll">
        {#each shown as p (p.pid)}
          {@const sel = p.pid === selected}
          <button class="dsp-row dsp-p" class:sel type="button" aria-pressed={sel} title={p.cmd} onclick={() => ((selected = p.pid), (ask = null), (note = ''))}>
            <span class="ell">{procName(p.cmd)}</span>
            <span class="num">{p.pid}</span>
            <span class="ell">{p.user}</span>
            <span class="num"><span class="dsp-heat" style={warm(heat(p.cpu, 50), sel)}>{num(p.cpu)}</span></span>
            <span class="num"><span class="dsp-heat" style={warm(heat(p.rss, 1024 ** 3), sel)}>{Math.round(p.rss / 1048576)} {$tr('disp.mb')}</span></span>
          </button>
        {:else}
          <p class="dsp-empty">{$tr('disp.empty')}</p>
        {/each}
      </div>
    </div>
  {/if}

  {#if ask}
    {@const target = rows.find((x) => x.pid === ask?.pid)}
    {#if target}
      <div class="dsp-confirm" role="alertdialog" aria-label={$tr('svc.confirm.title')}>
        <p>{askText(target, ask.signal)}</p>
        <button class="btn sm" type="button" disabled={busy} onclick={() => (ask = null)}>{$tr('win.cancel')}</button>
        <button class="btn sm danger" type="button" disabled={busy} onclick={() => void run()}>{$tr(ask.signal === 'KILL' ? 'disp.kill' : 'disp.term')}</button>
      </div>
    {/if}
  {/if}
</div>

<div class="dsp-foot">
  <p class="dsp-who">
    {#if current}<span class="muted">{$tr('disp.selectedProc')}</span> {procName(current.cmd)} · PID {current.pid}{:else}<span class="muted">{$tr('disp.pickProc')}</span>{/if}
    {#if note}<span class="note">{note}</span>{/if}
  </p>
  <button class="btn sm" type="button" disabled={!current || busy} onclick={() => current && (ask = { pid: current.pid, signal: 'TERM' })}>{$tr('disp.term')}</button>
  <button class="btn sm danger" type="button" disabled={!current || busy} onclick={() => current && (ask = { pid: current.pid, signal: 'KILL' })}>{$tr('disp.kill')}</button>
</div>
