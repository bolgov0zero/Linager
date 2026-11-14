<script lang="ts">
  import { untrack } from 'svelte';
  import { api, type ServerDetail, type ServiceAction, type ServiceUnit } from '../../../api';
  import { tr as tStore } from '../../../i18n';
  import Modal from '../../Modal.svelte';
  import ActionMenu from '../../ui/ActionMenu.svelte';
  import { showToast } from '../../toast';
  import {
    confirmText,
    errorText,
    isDangerous,
    needsConfirmation,
    stateKind,
    unavailableKind,
    unitBase,
    visibleServices,
    type ServiceFilter,
    type StateKind,
  } from './svc';

  let { server }: { server: ServerDetail } = $props();

  const POLL_MS = 10_000;
  const FILTERS: ServiceFilter[] = ['all', 'running', 'stopped', 'failed'];
  const FILTER_KEYS: Record<ServiceFilter, string> = {
    all: 'svc.filter.all',
    running: 'svc.filter.running',
    stopped: 'svc.filter.stopped',
    failed: 'svc.filter.failed',
  };
  const AUTO_KEYS = ['enabled', 'disabled', 'static', 'masked'];
  const STATE_KEYS: Record<StateKind, string | null> = {
    running: 'svc.state.running',
    failed: 'svc.state.failed',
    stopped: 'svc.state.stopped',
    other: null,
  };

  let units = $state<ServiceUnit[]>([]);
  let ignored = $state<string[]>([]);
  let loadState = $state<'loading' | 'ready' | 'offline' | 'disabled' | 'error'>('loading');
  let filter = $state<ServiceFilter>('all');
  let query = $state('');
  let busy = $state<string | null>(null);
  let ask = $state<{ unit: string; action: ServiceAction } | null>(null);

  let reqSeq = 0;

  async function load(id: string) {
    const req = ++reqSeq;
    try {
      const res = await api.services(id);
      if (req !== reqSeq) return;
      units = res.services;
      loadState = 'ready';
      void api.servicesIgnored(id).then((r) => (req === reqSeq ? (ignored = r.ignored) : undefined)).catch(() => undefined);
    } catch (e) {
      if (req !== reqSeq) return;
      loadState = unavailableKind(e) ?? 'error';
    }
  }

  // Load on mount and when the server changes, then poll while the tab is open.
  $effect(() => {
    const id = server.id;
    untrack(() => {
      loadState = 'loading';
      void load(id);
    });
    const timer = setInterval(() => untrack(() => void load(id)), POLL_MS);
    return () => clearInterval(timer);
  });

  const unreachable = $derived(!server.online || loadState === 'offline' || loadState === 'disabled');
  const counts = $derived({
    all: units.length,
    running: units.filter((u) => stateKind(u) === 'running').length,
    stopped: units.filter((u) => stateKind(u) === 'stopped').length,
    failed: units.filter((u) => stateKind(u) === 'failed').length,
  });
  const shown = $derived(visibleServices(units, filter, query));

  function autoLabel(value: string): string {
    return AUTO_KEYS.includes(value) ? $tStore(`svc.auto.${value}`) : value;
  }

  function stateLabel(unit: ServiceUnit): string {
    const key = STATE_KEYS[stateKind(unit)];
    return key ? $tStore(key) : unit.active;
  }

  async function execute(unit: string, action: ServiceAction, confirm: boolean) {
    busy = `${unit}:${action}`;
    try {
      await api.serviceAction(server.id, unit, action, confirm);
      showToast($tStore('svc.done', { action: $tStore(`svc.act.${action}`), unit }));
      await load(server.id);
    } catch (e) {
      if (!confirm && needsConfirmation(e)) {
        ask = { unit, action };
      } else {
        showToast(errorText(e, $tStore));
      }
    } finally {
      busy = null;
    }
  }

  function request(unit: string, action: ServiceAction) {
    if (isDangerous(unit, action)) {
      ask = { unit, action };
      return;
    }
    void execute(unit, action, false);
  }

  function confirmAction() {
    const pending = ask;
    ask = null;
    if (pending) void execute(pending.unit, pending.action, true);
  }

  function isBusy(unit: string, action: ServiceAction): boolean {
    return busy === `${unit}:${action}`;
  }

  async function setIgnored(unit: string, ignore: boolean) {
    try {
      await api.serviceIgnore(server.id, unit, ignore);
      ignored = ignore ? [...ignored, unit] : ignored.filter((u) => u !== unit);
      showToast($tStore(ignore ? 'svc.ignoredDone' : 'svc.unignoredDone', { unit }));
    } catch (e) {
      showToast(errorText(e, $tStore));
    }
  }

  function menuItems(unit: ServiceUnit, kind: StateKind) {
    const toggle: ServiceAction = unit.enabled === 'enabled' ? 'disable' : 'enable';
    const canToggle = unit.enabled === 'enabled' || unit.enabled === 'disabled';
    const lock = unreachable || busy !== null;
    const isIgnored = ignored.includes(unit.name);
    return [
      { key: 'start', label: $tStore('svc.act.start'), disabled: lock || kind === 'running', onselect: () => request(unit.name, 'start') },
      { key: 'restart', label: $tStore('svc.act.restart'), disabled: lock || kind === 'stopped', onselect: () => request(unit.name, 'restart') },
      { key: 'stop', label: $tStore('svc.act.stop'), disabled: lock || kind === 'stopped', onselect: () => request(unit.name, 'stop') },
      { key: 'toggle', label: $tStore(`svc.act.${toggle}`), disabled: lock || !canToggle, onselect: () => request(unit.name, toggle) },
      {
        key: 'ignore',
        label: $tStore(isIgnored ? 'svc.act.unignore' : 'svc.act.ignore'),
        disabled: server.online === false,
        onselect: () => void setIgnored(unit.name, !isIgnored),
      },
    ];
  }
</script>

<div class="svc">
  <div class="toolbar">
    <div class="filters" role="group" aria-label={$tStore('svc.filter.all')}>
      {#each FILTERS as f (f)}
        <button class="chip" type="button" aria-pressed={filter === f} onclick={() => (filter = f)}>
          {$tStore(FILTER_KEYS[f])} <span class="count">{counts[f]}</span>
        </button>
      {/each}
    </div>
    <input class="input search" type="search" placeholder={$tStore('svc.search')} aria-label={$tStore('svc.search')} bind:value={query} />
  </div>

  {#if loadState === 'offline'}
    <p class="notice" role="status">{$tStore('svc.offline')}</p>
  {:else if loadState === 'disabled'}
    <p class="notice" role="status">{$tStore('svc.disabled')}</p>
  {/if}

  {#if loadState === 'loading' && units.length === 0}
    <p class="muted">{$tStore('common.loading')}</p>
  {:else if loadState === 'error' && units.length === 0}
    <div class="state">
      <p class="error" role="alert">{$tStore('svc.loadFailed')}</p>
      <div class="row">
        <button class="btn" type="button" onclick={() => void load(server.id)}>{$tStore('svc.retry')}</button>
      </div>
    </div>
  {:else if units.length === 0 && loadState === 'ready'}
    <p class="muted">{$tStore('svc.empty')}</p>
  {:else}
    <p class="meta muted small">{$tStore('svc.count', { n: units.length })}</p>

    {#if shown.length === 0}
      <p class="muted">{$tStore('svc.noMatch')}</p>
    {:else}
      <div class="table-wrap">
        <table class="tbl">
          <thead>
            <tr>
              <th scope="col">{$tStore('svc.col.name')}</th>
              <th scope="col">{$tStore('svc.col.state')}</th>
              <th scope="col">{$tStore('svc.col.autostart')}</th>
              <th scope="col" class="right">{$tStore('svc.col.actions')}</th>
            </tr>
          </thead>
          <tbody>
            {#each shown as unit (unit.name)}
              {@const kind = stateKind(unit)}
              {@const isIgnored = ignored.includes(unit.name)}
              <tr class:failed={kind === 'failed' && !isIgnored}>
                <td>
                  <div class="name">
                    <span class="unit mono">{unit.name}</span>
                    {#if unit.description}<span class="desc muted small">{unit.description}</span>{/if}
                  </div>
                </td>
                <td>
                  <span class="badge state {kind}">{stateLabel(unit)}</span>
                  {#if isIgnored}<span class="badge state other" title={$tStore('svc.ignoreHint')}>{$tStore('svc.badge.ignored')}</span>{/if}
                </td>
                <td>
                  <span class="badge auto {unit.enabled === 'enabled' ? 'on' : unit.enabled === 'masked' ? 'warn' : 'off'}">
                    {autoLabel(unit.enabled)}
                  </span>
                </td>
                <td class="actions">
                  <ActionMenu label={$tStore('svc.menu')} items={menuItems(unit, kind)} disabled={busy !== null} />
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {/if}
  {/if}
</div>

{#if ask}
  {@const pending = ask}
  <Modal title={$tStore('svc.confirm.title')} labelId="svc-confirm-title" onclose={() => (ask = null)}>
    {#snippet children()}
      <p class="confirm-text">{confirmText(pending.unit, pending.action, $tStore)}</p>
      <p class="mono small muted">{unitBase(pending.unit)} · {$tStore(`svc.act.${pending.action}`)}</p>
      <div class="row end">
        <button class="btn" type="button" onclick={() => (ask = null)}>{$tStore('common.cancel')}</button>
        <button class="btn danger" type="button" onclick={confirmAction}>{$tStore('svc.confirm.ok')}</button>
      </div>
    {/snippet}
  </Modal>
{/if}

<style>
  .svc {
    display: flex;
    flex-direction: column;
    gap: 12px;
    min-width: 0;
  }

  .toolbar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
  }

  .filters {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }

  .count {
    display: inline-block;
    min-width: 1.6em;
    margin-left: 4px;
    padding: 0 4px;
    border: 1px solid currentColor;
    font-size: 12px;
    text-align: center;
  }

  .search {
    max-width: 280px;
  }

  .meta {
    margin: 0;
  }

  .notice {
    margin: 0;
    padding: 8px 12px;
    border: 2px solid var(--warn);
    color: var(--warn);
  }

  .state {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 12px;
  }

  .table-wrap {
    overflow-x: auto;
  }

  .tbl {
    width: 100%;
    min-width: 680px;
    border-collapse: collapse;
  }

  .tbl th {
    padding: 8px 10px;
    text-align: left;
    font-weight: 400;
    font-size: 12px;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--muted);
    border-bottom: 2px solid var(--line);
  }

  .tbl td {
    padding: 10px;
    vertical-align: middle;
    border-bottom: 1px dashed var(--seg-off);
  }

  .tbl tr.failed td {
    background: color-mix(in srgb, var(--crit) 8%, transparent);
  }

  .right {
    text-align: right;
  }

  .name {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 220px;
  }

  .unit {
    font-variation-settings: "ELSH" 40;
    font-family: var(--font-display);
    font-size: 15px;
  }

  .desc {
    overflow-wrap: anywhere;
  }

  .badge {
    display: inline-block;
    padding: 1px 6px;
    border: 2px solid currentColor;
    font-size: 12px;
    white-space: nowrap;
  }

  .badge.running,
  .badge.on {
    color: var(--good);
  }

  .badge.failed {
    color: var(--crit);
  }

  .badge.warn {
    color: var(--warn);
  }

  .badge.stopped,
  .badge.off,
  .badge.other {
    color: var(--muted);
  }

  /* All four buttons stay on one line so every row has the same height; narrow screens scroll the table. */
  .actions {
    width: 1%;
    text-align: right;
    white-space: nowrap;
  }

  .btns {
    display: inline-flex;
    flex-wrap: nowrap;
    justify-content: flex-end;
    gap: 6px;
  }

  .small-btn {
    padding: 4px 8px;
    font-size: 13px;
    box-shadow: 2px 2px 0 var(--shade);
  }

  .confirm-text {
    margin: 0;
  }

  @media (max-width: 600px) {
    .search {
      max-width: none;
      width: 100%;
    }
  }

  /* Phones: every service becomes a small block instead of a wide table. */
  @media (max-width: 600px) {
    .tbl {
      min-width: 0;
    }

    .tbl thead {
      position: absolute;
      width: 1px;
      height: 1px;
      overflow: hidden;
      clip: rect(0 0 0 0);
    }

    .tbl,
    .tbl tbody {
      display: block;
    }

    .tbl tr {
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      gap: 6px 8px;
      padding: 8px 4px;
      border-bottom: 1px dashed var(--seg-off);
    }

    .tbl td {
      padding: 0;
      border: 0;
    }

    .tbl td:first-child {
      flex: 1 0 100%;
      min-width: 0;
    }

    .tbl td.actions {
      flex: none;
      width: auto;
      margin-left: auto;
    }

    .tbl tr.failed {
      background: color-mix(in srgb, var(--crit) 8%, transparent);
    }

    .tbl tr.failed td {
      background: none;
    }

    .name {
      min-width: 0;
    }
  }
</style>
