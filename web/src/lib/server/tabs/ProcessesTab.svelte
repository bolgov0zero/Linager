<script lang="ts">
  import { get } from 'svelte/store';
  import { api, ApiError, type ProcessInfo, type ServerDetail } from '../../../api';
  import { tr } from '../../../i18n';
  import Modal from '../../Modal.svelte';
  import { showToast } from '../../toast';
  import { formatBytes } from '../format';

  let { server }: { server: ServerDetail } = $props();

  type Sort = 'cpu' | 'mem';
  type Problem = 'offline' | 'disabled' | 'error';

  const REFRESH_MS = 3000;

  let sort = $state<Sort>('cpu');
  let count = $state(10);
  let rows = $state<ProcessInfo[]>([]);
  let loaded = $state(false);
  let problem = $state<Problem | null>(null);
  let victim = $state<ProcessInfo | null>(null);
  let force = $state(false);
  let killing = $state(false);

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
        rows = list.processes;
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
    <div class="bar">
      <div class="row">
        <button class="chip" type="button" aria-pressed={sort === 'cpu'} onclick={() => (sort = 'cpu')}>
          {$tr('proc.sort.cpu')}
        </button>
        <button class="chip" type="button" aria-pressed={sort === 'mem'} onclick={() => (sort = 'mem')}>
          {$tr('proc.sort.mem')}
        </button>
        <label class="count">
          <span class="muted small">{$tr('proc.show')}</span>
          <select class="input" bind:value={count}>
            <option value={10}>10</option>
            <option value={20}>20</option>
            <option value={50}>50</option>
          </select>
        </label>
      </div>
      <p class="muted small">{$tr('proc.refresh', { n: count })}</p>
    </div>

    {#if problem === 'error'}
      <p class="error" role="alert">{$tr('proc.loadFailed')}</p>
    {:else if loaded && rows.length === 0}
      <p class="muted">{$tr('proc.empty')}</p>
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
              <tr>
                <td class="num">{p.pid}</td>
                <td>{p.user}</td>
                <td class="num">{p.cpu.toFixed(1)}</td>
                <td class="num">
                  {p.mem_pct.toFixed(1)}
                  <span class="muted small">{formatBytes(p.rss)}</span>
                </td>
                <td>{p.state}</td>
                <td class="cmd"><span class="ellipsis" title={p.cmd}>{p.cmd}</span></td>
                <td class="action">
                  <button class="btn" type="button" onclick={() => (victim = p)}>{$tr('proc.kill')}</button>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {/if}
  {/if}
</div>

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
    gap: 12px;
    min-width: 0;
  }

  .bar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
  }

  .count {
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .count .input {
    width: auto;
  }

  .note {
    margin: 0;
    padding: 24px 0;
    text-align: center;
    color: var(--muted);
  }

  .table-wrap {
    overflow-x: auto;
  }

  table {
    width: 100%;
    border-collapse: collapse;
    font-size: 12px;
  }

  th,
  td {
    padding: 8px 10px;
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
    max-width: 320px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .action {
    text-align: right;
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
    font-size: 12px;
  }

  .check {
    display: flex;
    align-items: center;
    gap: 8px;
    cursor: pointer;
  }

  .check input {
    width: 16px;
    height: 16px;
    margin: 0;
    accent-color: var(--accent);
  }

  @media (max-width: 600px) {
    .ellipsis {
      max-width: 180px;
    }
  }
</style>
