<script lang="ts">
  import { untrack } from 'svelte';
  import { api, type ServerDetail, type ServiceAction, type ServiceUnit } from '../../../api';
  import { tr as tStore } from '../../../i18n';
  import Modal from '../../Modal.svelte';
  import ActionMenu from '../../ui/ActionMenu.svelte';
  import ContextMenu, { type MenuItem } from '../../ui/ContextMenu.svelte';
  import PixIcon from '../../ui/PixIcon.svelte';
  import { isPhone } from '../../ui/viewport';
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
  /** Phone layout: the action sheet of one service. */
  let sheet = $state<{ label: string; items: MenuItem[] } | null>(null);

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

  function openSheet(unit: ServiceUnit, kind: StateKind): void {
    sheet = {
      label: unit.name,
      items: menuItems(unit, kind).map((i) => ({ ...i, danger: i.key === 'stop' })),
    };
  }
</script>

<div class="svc">
  {#if $isPhone}
    <div class="stick">
      <input class="input" type="search" placeholder={$tStore('svc.search')} aria-label={$tStore('svc.search')} bind:value={query} />
      <div class="seg" role="group" aria-label={$tStore('svc.filter.all')}>
        {#each FILTERS as f (f)}
          <button type="button" aria-pressed={filter === f} onclick={() => (filter = f)}>
            {$tStore(FILTER_KEYS[f])} {counts[f]}
          </button>
        {/each}
      </div>
    </div>
  {:else}
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

  {/if}

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
    {#if !$isPhone}<p class="meta muted small">{$tStore('svc.count', { n: units.length })}</p>{/if}

    {#if shown.length === 0}
      <p class="muted">{$tStore('svc.noMatch')}</p>
    {:else}
      {#if $isPhone}
      <div class="list">
        {#each shown as unit (unit.name)}
          {@const kind = stateKind(unit)}
          {@const isIgnored = ignored.includes(unit.name)}
          {@const fail = kind === 'failed' && !isIgnored}
          <div class="li" class:bad={fail}>
            <div class="mn">
              <div class="l1"><b>{unit.name}</b></div>
              {#if unit.description}<div class="l2">{unit.description}</div>{/if}
              {#if unit.enabled !== 'enabled' && unit.enabled !== 'static'}
                <div class="l2">{$tStore('svc.col.autostart')}: {autoLabel(unit.enabled)}</div>
              {/if}
            </div>
            <span class="badge state {kind}">{stateLabel(unit)}</span>
            {#if isIgnored}<span class="badge state other">{$tStore('svc.badge.ignored')}</span>{/if}
            <button class="more" type="button" aria-label="{$tStore('svc.menu')}: {unit.name}" disabled={busy !== null} onclick={() => openSheet(unit, kind)}>
              <PixIcon name="more" />
            </button>
            {#if fail}
              <button class="btn restart" type="button" disabled={unreachable || busy !== null} onclick={() => request(unit.name, 'restart')}>
                {$tStore('svc.act.restart')}
              </button>
            {/if}
          </div>
        {/each}
      </div>
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
  {/if}
</div>

{#if sheet}
  <ContextMenu x={0} y={0} items={sheet.items} label={sheet.label} onclose={() => (sheet = null)} />
{/if}

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
    gap: 11px;
    min-width: 0;
  }

  .toolbar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 7px;
  }

  .filters {
    display: flex;
    flex-wrap: wrap;
    gap: 5px;
  }

  .count {
    display: inline-block;
    min-width: 1.6em;
    margin-left: 4px;
    padding: 0 4px;
    border: 1px solid currentColor;
    font-size: 11px;
    text-align: center;
  }

  .search {
    max-width: 252px;
  }

  .meta {
    margin: 0;
  }

  .notice {
    margin: 0;
    padding: 7px 11px;
    border: 2px solid var(--warn);
    color: var(--warn);
  }

  .state {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 11px;
  }

  .table-wrap {
    overflow-x: auto;
  }

  .tbl {
    width: 100%;
    min-width: 612px;
    border-collapse: collapse;
  }

  .tbl th {
    padding: 7px 9px;
    text-align: left;
    font-weight: 400;
    font-size: 11px;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--muted);
    border-bottom: 2px solid var(--line);
  }

  .tbl td {
    padding: 9px;
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
    min-width: 198px;
  }

  .unit {
    font-variation-settings: "ELSH" 40;
    font-family: var(--font-display);
    font-size: 14px;
  }

  .desc {
    overflow-wrap: anywhere;
  }

  .badge {
    display: inline-block;
    padding: 1px 5px;
    border: 2px solid currentColor;
    font-size: 11px;
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
    gap: 5px;
  }

  .small-btn {
    padding: 4px 7px;
    font-size: 12px;
    box-shadow: 2px 2px 0 var(--shade);
  }

  .confirm-text {
    margin: 0;
  }

  /* ---- Phones: search and filter pinned under the header, one row per service ---- */
  .stick {
    position: sticky;
    top: calc(var(--top-h) - 7px);
    z-index: 9;
    display: flex;
    flex-direction: column;
    gap: 7px;
    margin-bottom: -11px;
    padding: 7px 0 18px;
    background: var(--bg);
  }

  .seg {
    display: flex;
    background: var(--surface);
    border: 2px solid var(--line);
    box-shadow: 3px 3px 0 var(--shade);
  }

  .seg button {
    flex: 1;
    min-width: 0;
    height: 44px;
    padding: 0 2px;
    overflow: hidden;
    background: none;
    border: 0;
    border-left: 2px solid var(--line);
    color: var(--ink);
    cursor: pointer;
    font-family: var(--font-display);
    font-size: 11px;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .seg button:first-child {
    border-left: 0;
  }

  .seg button[aria-pressed='true'] {
    background: var(--ink);
    color: var(--surface);
  }

  .list {
    background: var(--surface);
    border: 2px solid var(--line);
    box-shadow: 3px 3px 0 var(--shade);
  }

  .li {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 5px 7px;
    min-height: 56px;
    padding: 5px 4px 5px 11px;
    border-top: 2px dashed var(--seg-off);
  }

  .li:first-child {
    border-top: 0;
  }

  .li.bad {
    box-shadow: inset 4px 0 0 var(--crit);
  }

  .li .mn {
    flex: 1;
    min-width: 0;
  }

  .li .l1 {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .li .l1 b {
    font-weight: 400;
    font-size: 13px;
  }

  .li .l2 {
    overflow: hidden;
    color: var(--muted);
    font-size: 10px;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .li .badge {
    flex: none;
  }

  .more {
    flex: none;
    display: flex;
    align-items: center;
    justify-content: center;
    width: 44px;
    height: 44px;
    padding: 0;
    background: var(--surface);
    border: 2px solid var(--line);
    box-shadow: 2px 2px 0 var(--shade);
    color: var(--ink);
    cursor: pointer;
  }

  .more:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .restart {
    flex: 1 0 100%;
    margin: 0 4px 4px 0;
  }
</style>
