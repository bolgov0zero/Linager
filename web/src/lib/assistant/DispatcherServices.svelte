<script lang="ts">
  import { api, type ServiceAction, type ServiceUnit } from '../../api';
  import { tr } from '../../i18n';
  import { confirmText, errorText, isDangerous, needsConfirmation, stateKind, unavailableKind, type ServiceFilter } from '../server/tabs/svc';
  import { get } from 'svelte/store';
  import { showToast } from '../toast';
  import { nextSort, serviceActions, sortServices, type SortState, type SvcKey } from './dispatcher';

  /** The "Services" tab of the Dispatcher: the services of Windows, sorted by a column, and start, restart and stop of the picked one. */
  let { serverId, query, active }: { serverId: string; query: string; active: boolean } = $props();

  const POLL_MS = 10_000;
  const FILTERS: ServiceFilter[] = ['all', 'running', 'stopped', 'failed'];

  let units = $state<ServiceUnit[]>([]);
  let loaded = $state(false);
  let problem = $state<'offline' | 'disabled' | 'error' | null>(null);
  let filter = $state<ServiceFilter>('all');
  let sort = $state<SortState<SvcKey>>({ key: 'state', dir: 'asc' });
  let selected = $state<string | null>(null);
  let ask = $state<{ unit: string; action: ServiceAction } | null>(null);
  let busy = $state(false);
  let note = $state('');

  let refreshNow: () => void = () => {};

  $effect(() => {
    const id = serverId;
    if (!active) return;
    let alive = true;
    const run = async (): Promise<void> => {
      try {
        const res = await api.services(id);
        if (!alive) return;
        units = res.services;
        problem = null;
      } catch (e) {
        if (!alive) return;
        problem = unavailableKind(e) ?? 'error';
      } finally {
        if (alive) loaded = true;
      }
    };
    refreshNow = () => void run();
    void run();
    const timer = setInterval(() => void run(), POLL_MS);
    return () => {
      alive = false;
      clearInterval(timer);
    };
  });

  const counts = $derived({
    all: units.length,
    running: units.filter((u) => stateKind(u) === 'running').length,
    stopped: units.filter((u) => stateKind(u) === 'stopped').length,
    failed: units.filter((u) => stateKind(u) === 'failed').length,
  });
  const q = $derived(query.trim().toLowerCase());
  const shown = $derived(
    sortServices(
      units.filter((u) => (filter === 'all' || stateKind(u) === filter) && (!q || u.name.toLowerCase().includes(q) || u.description.toLowerCase().includes(q))),
      sort,
    ),
  );
  const current = $derived(selected === null ? undefined : units.find((u) => u.name === selected));
  const actions = $derived(serviceActions(current));

  const cols: { key: SvcKey; label: string }[] = [
    { key: 'name', label: 'svc.col.name' },
    { key: 'state', label: 'svc.col.state' },
    { key: 'auto', label: 'svc.col.autostart' },
  ];
  const kindBadge = { running: 'ok', failed: 'crit', stopped: 'mut', other: 'mut' } as const;
  const stateKey = { running: 'svc.state.running', failed: 'svc.state.failed', stopped: 'svc.state.stopped', other: '' } as const;
  const AUTO = ['enabled', 'disabled', 'static', 'masked'];
  const autoLabel = (v: string): string => (AUTO.includes(v) ? $tr(`svc.auto.${v}`) : v);

  async function execute(unit: string, action: ServiceAction, confirm: boolean): Promise<void> {
    busy = true;
    try {
      await api.serviceAction(serverId, unit, action, confirm);
      note = $tr('svc.done', { action: $tr(`svc.act.${action}`), unit });
      ask = null;
      refreshNow();
    } catch (e) {
      if (!confirm && needsConfirmation(e)) ask = { unit, action };
      else {
        ask = null;
        showToast(errorText(e, get(tr)));
      }
    } finally {
      busy = false;
    }
  }

  function request(action: ServiceAction): void {
    if (!current) return;
    note = '';
    if (isDangerous(current.name, action)) ask = { unit: current.name, action };
    else void execute(current.name, action, false);
  }

  const buttonClass = (a: 'start' | 'restart' | 'stop'): string => (a === 'start' ? 'btn sm primary' : a === 'stop' ? 'btn sm danger' : 'btn sm');
</script>

<div class="dsp-pane">
  {#if problem === 'offline' || problem === 'disabled'}
    <p class="dsp-empty">{$tr(problem === 'offline' ? 'svc.offline' : 'svc.disabled')}</p>
  {:else if problem === 'error' && !loaded}
    <p class="dsp-empty">{$tr('common.genericError')}</p>
  {:else if !loaded}
    <p class="dsp-empty">{$tr('common.loading')}</p>
  {:else}
    <div class="dsp-filters">
      {#each FILTERS as f (f)}
        <button class="chip" type="button" aria-pressed={filter === f} onclick={() => (filter = f)}>{$tr(`svc.filter.${f}`)} · {counts[f]}</button>
      {/each}
    </div>
    <div class="dsp-list">
      <div class="dsp-lh dsp-s">
        {#each cols as c (c.key)}
          <button
            class="dsp-th"
            class:on={sort.key === c.key}
            type="button"
            title={$tr('disp.sortHint')}
            onclick={() => (sort = nextSort(sort, c.key, true))}
          >
            <span class="dsp-tot"></span>
            <span class="dsp-lab">{$tr(c.label)}<i class="dsp-arr" class:up={sort.key === c.key && sort.dir === 'asc'} class:down={sort.key === c.key && sort.dir === 'desc'} class:none={sort.key !== c.key}></i></span>
          </button>
        {/each}
      </div>
      <div class="dsp-scroll">
        {#each shown as u (u.name)}
          {@const kind = stateKind(u)}
          {@const sel = u.name === selected}
          <button class="dsp-row dsp-s" class:sel class:fail={kind === 'failed'} type="button" aria-pressed={sel} onclick={() => ((selected = u.name), (ask = null), (note = ''))}>
            <span style="min-width:0"><span class="ell">{u.name}</span><span class="sub">{u.description}</span></span>
            <span><span class="badge {kindBadge[kind]}">{stateKey[kind] ? $tr(stateKey[kind]) : u.active}</span></span>
            <span class="ell">{autoLabel(u.enabled)}</span>
          </button>
        {:else}
          <p class="dsp-empty">{$tr('disp.empty')}</p>
        {/each}
      </div>
    </div>
  {/if}

  {#if ask}
    <div class="dsp-confirm" role="alertdialog" aria-label={$tr('svc.confirm.title')}>
      <p>{confirmText(ask.unit, ask.action, get(tr))}</p>
      <button class="btn sm" type="button" disabled={busy} onclick={() => (ask = null)}>{$tr('win.cancel')}</button>
      <button class="btn sm danger" type="button" disabled={busy} onclick={() => ask && void execute(ask.unit, ask.action, true)}>{$tr('svc.confirm.ok')}</button>
    </div>
  {/if}
</div>

<div class="dsp-foot">
  <p class="dsp-who">
    {#if current}<span class="muted">{$tr('disp.selectedSvc')}</span> {current.name}{:else}<span class="muted">{$tr('disp.pickSvc')}</span>{/if}
    {#if note}<span class="note">{note}</span>{/if}
  </p>
  {#if current}
    {#each actions as a (a)}
      <button class={buttonClass(a)} type="button" disabled={busy} onclick={() => request(a)}>{$tr(`svc.act.${a}`)}</button>
    {/each}
  {:else}
    <button class="btn sm primary" type="button" disabled>{$tr('svc.act.start')}</button>
    <button class="btn sm danger" type="button" disabled>{$tr('svc.act.stop')}</button>
  {/if}
</div>
