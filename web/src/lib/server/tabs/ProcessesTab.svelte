<script lang="ts">
  import { get } from 'svelte/store';
  import Select from '../../ui/Select.svelte';
  import Segments from '../../ui/Segments.svelte';
  import { isPhone } from '../../ui/viewport';
  import { api, ApiError, type ProcessInfo, type ServerDetail } from '../../../api';
  import { tr } from '../../../i18n';
  import Modal from '../../Modal.svelte';
  import { showToast } from '../../toast';
  import { formatBytes } from '../format';

  let { server }: { server: ServerDetail } = $props();

  type Sort = 'cpu' | 'mem' | 'name';
  type Problem = 'offline' | 'disabled' | 'error';

  const REFRESH_MS = 3000;
  // "all" asks the agent for its cap.
  const ALL = 2000;
  const COUNTS = [10, 20, 50, 100, ALL];
  const countOptions = $derived(COUNTS.map((n) => ({ value: n, label: n === ALL ? $tr('proc.all') : String(n) })));
  // Process states from /proc: the letter, and the warn tint for a zombie or a process stuck in I/O.
  const STATES = ['S', 'R', 'I', 'D', 'Z', 'T'];
  const WARN_STATES = ['D', 'Z'];

  let sort = $state<Sort>('cpu');
  let count = $state(10);
  const refreshText = $derived(count === ALL ? $tr('proc.refreshAll') : $tr('proc.refresh', { n: count }));
  let rows = $state<ProcessInfo[]>([]);
  let loaded = $state(false);
  let problem = $state<Problem | null>(null);
  let victim = $state<ProcessInfo | null>(null);
  let force = $state(false);
  let killing = $state(false);
  // Phones: the row that was tapped, its action sheet is open. The list stops refreshing meanwhile, so the rows do
  // not move under the finger.
  let sheet = $state<ProcessInfo | null>(null);

  // Polls the top list; restarts when the server, sort or count changes.
  $effect(() => {
    const id = server.id;
    const online = server.online;
    const s = sort;
    const n = count;
    if (!online) {
      problem = 'offline';
      return;
    }

    let active = true;
    let inFlight = false;

    const run = async () => {
      if (inFlight) return;
      inFlight = true;
      try {
        const list = await api.processes(id, s, n);
        if (!active) return;
        if (!sheet) rows = list.processes;
        problem = null;
      } catch (e) {
        if (!active) return;
        if (e instanceof ApiError && e.status === 503) problem = 'offline';
        else if (e instanceof ApiError && e.status === 501) problem = 'disabled';
        else problem = 'error';
      } finally {
        inFlight = false;
        if (active) loaded = true;
      }
    };

    void run();
    const timer = setInterval(() => void run(), REFRESH_MS);
    return () => {
      active = false;
      clearInterval(timer);
    };
  });

  function ask(target: ProcessInfo, hard: boolean): void {
    sheet = null;
    force = hard;
    victim = target;
  }

  /** The figure a phone row shows and draws: the one the list is sorted by. */
  const metric = (p: ProcessInfo): number => (sort === 'mem' ? p.mem_pct : p.cpu);

  function stateTitle(s: string): string {
    const translate = get(tr);
    return STATES.includes(s) ? translate(`proc.state.${s}`) : translate('proc.state.other', { s });
  }

  /** The command as shown: the executable name (normal weight), then its arguments (muted). Same rule as the agent. */
  function splitCmd(cmd: string): { exe: string; args: string } {
    const i = cmd.search(/\s/);
    const head = i < 0 ? cmd : cmd.slice(0, i);
    const args = i < 0 ? '' : cmd.slice(i);
    const exe = head.startsWith('/') || head.startsWith('./') ? head.slice(head.lastIndexOf('/') + 1) : head;
    return { exe, args };
  }

  function closeKill(): void {
    if (!killing) victim = null;
  }

  async function kill(target: ProcessInfo): Promise<void> {
    if (killing) return;
    killing = true;
    const translate = get(tr);
    try {
      await api.killProcess(server.id, target.pid, force ? 'KILL' : 'TERM');
      showToast(translate('proc.sent', { pid: target.pid }));
    } catch {
      showToast(translate('proc.killFailed', { pid: target.pid }));
    } finally {
      killing = false;
      force = false;
      victim = null;
    }
  }
</script>

<div class="proc">
  {#if problem === 'offline'}
    <p class="note">{$tr('proc.offline')}</p>
  {:else if problem === 'disabled'}
    <p class="note">{$tr('proc.disabled')}</p>
  {:else}
    <div class="bar" class:pbar={$isPhone}>
      <div class="row sorts">
        <button class="chip" type="button" aria-pressed={sort === 'cpu'} onclick={() => (sort = 'cpu')}>
          {$tr('proc.sort.cpu')}
        </button>
        <button class="chip" type="button" aria-pressed={sort === 'mem'} onclick={() => (sort = 'mem')}>
          {$tr('proc.sort.mem')}
        </button>
        <button class="chip" type="button" aria-pressed={sort === 'name'} onclick={() => (sort = 'name')}>
          {$tr('proc.sort.name')}
        </button>
        {#if !$isPhone}
          <label class="count">
            <span class="muted small">{$tr('proc.show')}</span>
            <Select bind:value={count} width="auto" options={countOptions} />
          </label>
        {/if}
      </div>
      {#if $isPhone}
        <div class="meta">
          <p class="muted small">{refreshText}</p>
          <label class="count">
            <span class="muted small">{$tr('proc.show')}</span>
            <Select bind:value={count} width="auto" ariaLabel={$tr('proc.show')} options={countOptions} />
          </label>
        </div>
      {:else}
        <p class="muted small">{refreshText}</p>
      {/if}
    </div>

    {#if problem === 'error'}
      <p class="error" role="alert">{$tr('proc.loadFailed')}</p>
    {:else if loaded && rows.length === 0}
      <p class="muted">{$tr('proc.empty')}</p>
    {:else}
      {#if $isPhone}
        <ul class="plist">
          {#each rows as p (p.pid)}
            <li>
              <button class="li" type="button" onclick={() => (sheet = p)}>
                <span class="mn">
                  <b class="l1" title={p.cmd}>{p.cmd}</b>
                  <span class="l2">{p.user} · PID {p.pid}{sort === 'mem' ? ` · ${formatBytes(p.rss)}` : ''}</span>
                </span>
                <span class="gauge">
                  <span class="val">{metric(p).toFixed(metric(p) < 10 ? 1 : 0)}%</span>
                  <Segments value={metric(p)} segments={10} />
                </span>
              </button>
            </li>
          {/each}
        </ul>
      {:else}
      <div class="table-wrap">
        <table>
          <thead>
            <tr>
              <th scope="col" class="num">{$tr('proc.col.pid')}</th>
              <th scope="col">{$tr('proc.col.user')}</th>
              <th scope="col" class="num">{$tr('proc.col.cpu')}</th>
              <th scope="col" class="num">{$tr('proc.col.ram')}</th>
              <th scope="col">{$tr('proc.col.state')}</th>
              <th scope="col">{$tr('proc.col.cmd')}</th>
              <th scope="col"><span class="sr">{$tr('proc.col.action')}</span></th>
            </tr>
          </thead>
          <tbody>
            {#each rows as p (p.pid)}
              {@const c = splitCmd(p.cmd)}
              <tr>
                <td class="num">{p.pid}</td>
                <td>{p.user}</td>
                <td class="num" data-label="CPU">
                  <span class="fig">
                    <span class="v">{p.cpu.toFixed(1)}</span>
                    <span class="meter"><Segments value={p.cpu} segments={10} /></span>
                  </span>
                </td>
                <td class="num" data-label="RAM">
                  <span class="fig">
                    <span class="v">{p.mem_pct.toFixed(1)}</span>
                    <span class="meter"><Segments value={p.mem_pct} segments={10} /></span>
                  </span>
                  <span class="muted small">{formatBytes(p.rss)}</span>
                </td>
                <td>
                  <span class="state" class:warn={WARN_STATES.includes(p.state)} title={stateTitle(p.state)}>{p.state}</span>
                </td>
                <td class="cmd">
                  <span class="ellipsis" title={p.cmd}><span class="exe">{c.exe}</span><span class="args">{c.args}</span></span>
                </td>
                <td class="action">
                  <button class="btn sm" type="button" title={$tr('proc.endBtn', { pid: p.pid })} onclick={() => (victim = p)}>{$tr('proc.kill')}</button>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
      {/if}
    {/if}
  {/if}
</div>

{#if sheet}
  {@const target = sheet}
  <Modal title={`${target.cmd.length > 30 ? `${target.cmd.slice(0, 29)}…` : target.cmd} · PID ${target.pid}`} labelId="proc-act-title" flush onclose={() => (sheet = null)}>
    <button class="act" type="button" onclick={() => ask(target, false)}>{$tr('m.proc.term')}</button>
    <button class="act dng" type="button" onclick={() => ask(target, true)}>{$tr('m.proc.kill')}</button>
  </Modal>
{/if}

{#if victim}
  {@const target = victim}
  <Modal title={$tr('proc.killTitle', { pid: target.pid })} labelId="proc-kill-title" onclose={closeKill}>
    <p>{$tr('proc.killText', { pid: target.pid, cmd: target.cmd })}</p>
    <label class="check">
      <input type="checkbox" bind:checked={force} disabled={killing} />
      <span>{$tr('proc.force')}</span>
    </label>
    {#if force}
      <p class="hint">{$tr('proc.forceHint')}</p>
    {/if}
    <div class="row end">
      <button class="btn" type="button" disabled={killing} onclick={closeKill}>{$tr('common.cancel')}</button>
      <button class="btn danger" type="button" disabled={killing} onclick={() => kill(target)}>
        {force ? $tr('proc.confirmKill') : $tr('proc.confirm')}
      </button>
    </div>
  </Modal>
{/if}

<style>
  .proc {
    display: flex;
    flex-direction: column;
    gap: 11px;
    min-width: 0;
  }

  .bar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 7px;
  }

  .count {
    display: flex;
    align-items: center;
    gap: 5px;
    margin-left: 5px;
  }


  .note {
    margin: 0;
    padding: 22px 0;
    text-align: center;
    color: var(--muted);
  }

  .table-wrap {
    overflow-x: auto;
  }

  table {
    width: 100%;
    border-collapse: collapse;
    font-size: 11px;
  }

  th,
  td {
    padding: 7px 9px;
    text-align: left;
    vertical-align: middle;
    border-bottom: 1px dashed var(--seg-off);
    white-space: nowrap;
  }

  th {
    color: var(--muted);
    font-weight: 400;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    border-bottom: 2px solid var(--line);
  }

  .num {
    text-align: right;
    font-variant-numeric: tabular-nums;
  }

  th.num {
    text-align: right;
  }

  .cmd {
    width: 100%;
    max-width: 0;
  }

  .ellipsis {
    display: block;
    max-width: 288px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .action {
    text-align: right;
  }

  tbody tr:hover td {
    background: var(--bg);
  }

  .fig {
    display: inline-flex;
    align-items: center;
    justify-content: flex-end;
    gap: 7px;
  }

  .v {
    min-width: 36px;
    text-align: right;
  }

  .meter {
    display: inline-block;
    width: 65px;
  }

  .args {
    color: var(--muted);
  }

  .state {
    display: inline-block;
    min-width: 18px;
    padding: 0 5px;
    border: 2px solid var(--line);
    background: var(--surface);
    font-size: 11px;
    line-height: 1.3;
    text-align: center;
  }

  .state.warn {
    border-color: var(--warn);
    color: var(--warn);
  }

  .btn.sm {
    min-height: 0;
    padding: 2px 7px;
    font-size: 11px;
    box-shadow: 2px 2px 0 var(--shade);
  }

  .sr {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip: rect(0 0 0 0);
  }

  .hint {
    margin: 0;
    color: var(--muted);
    font-size: 11px;
  }

  .check {
    display: flex;
    align-items: center;
    gap: 7px;
    cursor: pointer;
  }

  .check input {
    width: 14px;
    height: 14px;
    margin: 0;
    accent-color: var(--accent);
  }

  .plist {
    margin: 0;
    padding: 0;
    list-style: none;
  }

  /* Phones: a segmented sort control, then rows (command, user and PID, the figure with a bar); a tap opens the
     action sheet. */
  @media (max-width: 720px) {
    .bar.pbar {
      flex-direction: column;
      align-items: stretch;
      gap: 7px;
    }

    .sorts {
      display: flex;
      flex-wrap: nowrap;
      gap: 0;
    }

    .sorts .chip {
      flex: 1;
      box-shadow: none;
      border-left-width: 0;
    }

    .sorts .chip:first-child {
      border-left-width: 2px;
    }

    .meta {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 7px;
    }

    .meta p {
      margin: 0;
    }

    .plist {
      background: var(--surface);
      border: 2px solid var(--line);
      box-shadow: 3px 3px 0 var(--shade);
    }

    .plist li + li {
      border-top: 2px dashed var(--seg-off);
    }

    .li {
      display: flex;
      align-items: center;
      gap: 11px;
      width: 100%;
      min-height: 56px;
      padding: 7px 11px;
      background: none;
      border: 0;
      color: inherit;
      font: inherit;
      text-align: left;
      cursor: pointer;
    }

    .mn {
      display: flex;
      flex: 1;
      flex-direction: column;
      min-width: 0;
      gap: 2px;
    }

    .l1 {
      overflow: hidden;
      font-weight: 400;
      text-overflow: ellipsis;
      white-space: nowrap;
    }

    .l2 {
      overflow: hidden;
      color: var(--muted);
      font-size: 11px;
      text-overflow: ellipsis;
      white-space: nowrap;
    }

    .gauge {
      display: flex;
      flex: none;
      flex-direction: column;
      align-items: flex-end;
      gap: 4px;
      width: 92px;
    }

    .gauge .val {
      font-variant-numeric: tabular-nums;
    }

    .act {
      display: flex;
      align-items: center;
      gap: 9px;
      width: 100%;
      min-height: 52px;
      padding: 0 13px;
      background: none;
      border: 0;
      border-top: 2px dashed var(--seg-off);
      color: inherit;
      font: inherit;
      text-align: left;
      cursor: pointer;
    }

    .act:first-child {
      border-top: 0;
    }

    .act.dng {
      color: var(--crit);
    }
  }
</style>
