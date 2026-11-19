<script lang="ts">
  import { onMount } from 'svelte';
  import { get } from 'svelte/store';
  import { api, type RolloutMode, type UpdatesInfo } from '../api';
  import { locale, t, tr as trans } from '../i18n';
  import { showToast } from './toast';
  import Fold from './ui/Fold.svelte';
  import ServerPanel from './updates/ServerPanel.svelte';
  import CachePanel from './updates/CachePanel.svelte';
  import AgentsPanel from './updates/AgentsPanel.svelte';
  import HistoryPanel from './updates/HistoryPanel.svelte';
  import { errorText, fmtBytes } from './updates/format';

  let { onBack, onchanged, embedded = false }: { onBack?: () => void; onchanged?: () => void; embedded?: boolean } = $props();

  const FAST_MS = 2000;
  const SLOW_MS = 15000;

  /** Toast text in the current language (toasts are short-lived, so no reactivity is needed). */
  function say(key: string, vars?: Record<string, string | number>): string {
    return t(key, get(locale), vars);
  }

  let info = $state<UpdatesInfo | null>(null);
  let loadFailed = $state(false);
  /** The server answers no more, but an update was running: the service is restarting. */
  let restarting = $state(false);
  /** Set between starting the server update and the first poll that sees it. */
  let serverPending = $state(false);
  let pendingRunId: string | null = null;
  let checking = $state(false);
  let channelBusy = $state(false);
  let prefetching = $state<string | null>(null);

  const serverRunning = $derived(info?.server.update_run.state === 'running');
  const serverBusy = $derived(Boolean(serverRunning) || serverPending);
  const agentsBusy = $derived(
    info ? info.agents.some((agent) => agent.status === 'updating' || agent.status === 'queued') : false,
  );
  const rolloutRunning = $derived(info?.rollout?.state === 'running');
  const busy = $derived(restarting || serverBusy || agentsBusy || Boolean(rolloutRunning));

  let inflight = false;
  let destroyed = false;
  let wasBusy = false;
  let timer: ReturnType<typeof setTimeout> | undefined;

  /** Stores a fresh server state and tells the header counter when an update has finished. */
  function applyInfo(next: UpdatesInfo): void {
    if (serverPending && (next.server.update_run.state === 'running' || next.server.update_run.run_id !== pendingRunId)) {
      serverPending = false;
    }
    info = next;
    const nowBusy =
      serverPending ||
      next.server.update_run.state === 'running' ||
      next.agents.some((agent) => agent.status === 'updating' || agent.status === 'queued') ||
      next.rollout?.state === 'running';
    if (wasBusy && !nowBusy) onchanged?.();
    wasBusy = nowBusy;
  }

  async function refresh(): Promise<void> {
    if (inflight || destroyed) return;
    inflight = true;
    try {
      applyInfo(await api.updates());
      restarting = false;
      loadFailed = false;
    } catch (error) {
      if (serverBusy || restarting) {
        // The API drops while the service restarts: keep polling quietly.
        restarting = true;
      } else {
        loadFailed = true;
      }
    } finally {
      inflight = false;
    }
  }

  function scheduleNext(): void {
    clearTimeout(timer);
    if (destroyed || document.hidden) return;
    timer = setTimeout(() => void tick(), busy ? FAST_MS : SLOW_MS);
  }

  async function tick(): Promise<void> {
    await refresh();
    scheduleNext();
  }

  /** Refreshes now and restarts the polling clock. */
  function kick(): void {
    clearTimeout(timer);
    void tick();
  }

  onMount(() => {
    const onVisibility = (): void => {
      if (!document.hidden) kick();
    };
    document.addEventListener('visibilitychange', onVisibility);
    void tick();
    return () => {
      destroyed = true;
      clearTimeout(timer);
      document.removeEventListener('visibilitychange', onVisibility);
    };
  });

  async function check(): Promise<void> {
    checking = true;
    try {
      const next = await api.updatesCheck();
      applyInfo(next);
      if (next.server.available && next.server.available !== next.server.current) {
        showToast(say('upd.toast.found', { version: next.server.available }));
      } else {
        showToast(say('upd.toast.none'));
      }
    } catch (error) {
      showToast(say('upd.err.request', { error: errorText(error) }));
    } finally {
      checking = false;
    }
  }

  async function changeChannel(channel: 'stable' | 'beta'): Promise<void> {
    channelBusy = true;
    try {
      applyInfo(await api.updatesChannel(channel));
    } catch (error) {
      showToast(say('upd.err.request', { error: errorText(error) }));
    } finally {
      channelBusy = false;
    }
  }

  async function startServerUpdate(version: string): Promise<void> {
    pendingRunId = info?.server.update_run.run_id ?? null;
    serverPending = true;
    try {
      await api.updatesServer(version);
      kick();
    } catch (error) {
      serverPending = false;
      showToast(say('upd.err.request', { error: errorText(error) }));
    }
  }

  let pruning = $state(false);

  async function prune(): Promise<void> {
    pruning = true;
    try {
      const next = await api.updatesCachePrune();
      applyInfo(next);
      const done = next.pruned;
      if (done) showToast(say('upd.cache.pruneDone', { n: done.count, size: fmtBytes(done.bytes, get(trans)) }));
    } catch (error) {
      showToast(say('upd.err.prune', { error: errorText(error) }));
    } finally {
      pruning = false;
    }
  }

  async function prefetch(version: string): Promise<void> {
    prefetching = version;
    try {
      applyInfo(await api.updatesPrefetch(version));
    } catch (error) {
      showToast(say('upd.err.prefetch', { error: errorText(error) }));
    } finally {
      prefetching = null;
    }
  }

  async function runAgents(
    ids: string[] | 'all',
    mode: RolloutMode,
    stopOnError: boolean,
  ): Promise<boolean> {
    try {
      applyInfo(await api.updatesAgents(ids, mode, stopOnError));
      kick();
      return true;
    } catch (error) {
      showToast(say('upd.err.request', { error: errorText(error) }));
      return false;
    }
  }
</script>

<div class="upd-page">
  <header class="upd-head">
    <div class="upd-ttl">
      {#if !embedded && onBack}
        <button class="btn sm" type="button" onclick={onBack}>{$trans('upd.back')}</button>
      {/if}
      <h2 class="upd-title">{$trans('upd.title')}</h2>
    </div>
    {#if info}
      <span class="upd-sub">{$trans('upd.channel', { channel: info.channel })}</span>
    {/if}
  </header>

  {#if !info}
    <p class="upd-sub">{loadFailed ? $trans('upd.loadError') : $trans('upd.loading')}</p>
  {:else}
    {#if loadFailed}
      <p class="upd-err">{$trans('upd.loadError')}</p>
    {/if}

    <div class="upd-grid">
      <ServerPanel
        {info}
        busy={serverBusy}
        {restarting}
        {checking}
        channelBusy={channelBusy || serverBusy}
        oncheck={check}
        onchannel={changeChannel}
        onupdate={startServerUpdate}
      />
      <CachePanel cache={info.cache} prunable={info.cache_prunable ?? 0} {pruning} {prefetching} onprefetch={prefetch} onprune={prune} />
    </div>

    <AgentsPanel {info} onrun={runAgents} />

    <div class="upd-grid">
      <HistoryPanel history={info.history} />
      <Fold title={$trans('upd.cli.title')}>
        <pre class="upd-term"><code><span class="cmd">linager update --check</span>      <span class="dim">{$trans('upd.cli.check')}</span>
<span class="cmd">linager update</span>              <span class="dim">{$trans('upd.cli.update')}</span>
<span class="cmd">linager agent update</span>        <span class="dim">{$trans('upd.cli.agentUpdate')}</span>
<span class="cmd">linager agent status</span>        <span class="dim">{$trans('upd.cli.agentStatus')}</span></code></pre>
      </Fold>
    </div>
  {/if}
</div>

<style>
  /* Shared building blocks for the updates page. Global because the panels are separate components. */
  :global(.upd-page) {
    display: flex;
    flex-direction: column;
    gap: 14px;
  }

  .upd-head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 11px;
  }

  .upd-ttl {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 11px;
  }

  .upd-title {
    margin: 0;
    font-variation-settings: "ELSH" 40;
    font-family: var(--font-display);
    font-size: 27px;
    font-weight: 400;
    line-height: 1;
  }

  :global(.upd-grid) {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
    gap: 14px;
    align-items: start;
  }

  @media (max-width: 900px) {
    :global(.upd-grid) {
      grid-template-columns: minmax(0, 1fr);
    }
  }

  :global(.upd-panel) {
    display: flex;
    flex-direction: column;
    gap: 11px;
    min-width: 0;
    padding: 11px;
    border: 2px solid var(--line);
    background: var(--bg);
  }

  :global(.upd-h) {
    margin: 0;
    font-family: var(--font-display);
    font-size: 12px;
    font-weight: 400;
    text-transform: uppercase;
    letter-spacing: 0.08em;
    color: var(--muted);
  }

  :global(.upd-big) {
    font-variation-settings: "ELSH" 40;
    font-family: var(--font-display);
    font-size: 23px;
    line-height: 1.1;
    font-variant-numeric: tabular-nums;
  }

  :global(.upd-vers) {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 9px;
  }

  :global(.upd-kv) {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr);
    gap: 5px 13px;
    margin: 0;
    font-size: 11px;
  }

  :global(.upd-kv dt) {
    color: var(--muted);
  }

  :global(.upd-kv dd) {
    margin: 0;
    text-align: right;
    font-variant-numeric: tabular-nums;
    overflow-wrap: anywhere;
  }

  :global(.upd-select) {
    background: var(--surface);
    color: var(--ink);
    border: 2px solid var(--line);
    padding: 5px 7px;
    min-width: 0;
    font: inherit;
    font-size: 12px;
  }

  :global(.upd-select:disabled) {
    opacity: 0.5;
  }

  :global(.upd-list) {
    margin: 0;
    padding-left: 16px;
    font-size: 11px;
    display: flex;
    flex-direction: column;
    gap: 3px;
  }

  :global(.upd-acts) {
    display: flex;
    flex-wrap: nowrap;
    gap: 7px;
  }

  /* a narrow panel shrinks the buttons (the text is cut) instead of moving the last one to a second row */
  :global(.upd-acts .btn) {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  @media (max-width: 720px) {
    :global(.upd-acts) {
      flex-wrap: wrap;
    }
  }

  :global(.upd-acts-end) {
    justify-content: flex-end;
    margin-top: 7px;
  }

  :global(.upd-bar) {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 9px 13px;
  }

  :global(.upd-label),
  :global(.upd-check) {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    font-size: 11px;
    color: var(--muted);
  }

  :global(.upd-check input),
  :global(.upd-tbl input[type='checkbox']) {
    width: 14px;
    height: 14px;
    margin: 0;
    accent-color: var(--accent);
  }

  :global(.upd-intro) {
    margin: 0;
    font-size: 11px;
    color: var(--muted);
  }

  :global(.upd-tw) {
    overflow-x: auto;
  }

  :global(.upd-tbl) {
    width: 100%;
    border-collapse: collapse;
    font-size: 11px;
  }

  :global(.upd-tbl th) {
    font-family: var(--font-display);
    font-size: 11px;
    font-weight: 400;
    text-transform: uppercase;
    letter-spacing: 0.08em;
    color: var(--muted);
    text-align: left;
    padding: 5px 7px;
    border-bottom: 2px solid var(--line);
    white-space: nowrap;
  }

  :global(.upd-tbl td) {
    padding: 6px 7px;
    border-bottom: 2px dashed var(--seg-off);
    vertical-align: middle;
  }

  :global(.upd-tbl .upd-sel) {
    width: 32px;
  }

  :global(.upd-tbl .upd-r) {
    text-align: right;
    white-space: nowrap;
  }

  :global(.upd-agents td) {
    padding-top: 9px;
    padding-bottom: 9px;
  }

  :global(.upd-name) {
    font-variation-settings: "ELSH" 40;
    font-family: var(--font-display);
    font-size: 14px;
  }

  :global(.upd-sub) {
    color: var(--muted);
    font-size: 11px;
  }

  :global(.upd-mono) {
    font-variant-numeric: tabular-nums;
    overflow-wrap: anywhere;
  }

  :global(.upd-badge) {
    display: inline-block;
    border: 2px solid var(--muted);
    color: var(--muted);
    padding: 0 5px;
    font-size: 10px;
    line-height: 16px;
    white-space: nowrap;
  }

  :global(.upd-badge.ok) {
    border-color: var(--good);
    color: var(--good);
  }

  :global(.upd-badge.warn) {
    border-color: var(--warn);
    color: var(--warn);
  }

  :global(.upd-badge.crit) {
    border-color: var(--crit);
    color: var(--crit);
  }

  :global(.upd-badge.muted) {
    border-color: var(--muted);
    color: var(--muted);
  }

  :global(.upd-err),
  :global(.upd-err-inline) {
    margin: 0;
    color: var(--crit);
    font-size: 11px;
    overflow-wrap: anywhere;
  }

  :global(.upd-ok) {
    margin: 0;
    color: var(--good);
    font-size: 11px;
  }

  :global(.upd-hint) {
    margin: 0;
    color: var(--muted);
    font-size: 11px;
  }

  :global(.upd-modal-text) {
    margin: 0;
  }

  :global(.upd-run) {
    display: flex;
    flex-direction: column;
    gap: 7px;
    padding: 9px;
    border: 2px dashed var(--seg-off);
  }

  :global(.upd-steps) {
    margin: 0;
    padding: 0;
    list-style: none;
    display: flex;
    flex-direction: column;
    gap: 4px;
    font-size: 11px;
  }

  :global(.upd-step) {
    display: flex;
    align-items: center;
    gap: 7px;
  }

  :global(.upd-step.pending) {
    color: var(--muted);
  }

  :global(.upd-step.done .upd-ico) {
    color: var(--good);
  }

  :global(.upd-step.running) {
    color: var(--accent);
  }

  :global(.upd-step.failed .upd-ico) {
    color: var(--crit);
  }

  :global(.upd-step.unknown) {
    color: var(--accent);
  }

  :global(.upd-dots) {
    animation: upd-pulse 1.2s ease-in-out infinite;
  }

  :global(.upd-dur) {
    margin-left: auto;
    color: var(--muted);
    white-space: nowrap;
  }

  @keyframes -global-upd-pulse {
    50% {
      opacity: 0.25;
    }
  }

  @media (prefers-reduced-motion: reduce) {
    :global(.upd-dots),
    :global(.upd-spin) {
      animation: none;
    }
  }

  :global(.upd-ico) {
    display: inline-flex;
    justify-content: center;
    width: 13px;
  }

  :global(.upd-spin) {
    display: inline-block;
    width: 9px;
    height: 9px;
    border: 2px solid currentColor;
    border-right-color: transparent;
    border-radius: 50%;
    vertical-align: -1px;
    animation: upd-spin 0.8s linear infinite;
  }

  @keyframes -global-upd-spin {
    to {
      transform: rotate(360deg);
    }
  }

  :global(.upd-pb) {
    display: flex;
    gap: 2px;
    height: 9px;
    width: 99px;
    margin-bottom: 4px;
  }

  :global(.upd-pb i) {
    flex: 1;
    background: var(--seg-off);
  }

  :global(.upd-pb i.on) {
    background: var(--accent);
  }

  .upd-term {
    margin: 0;
    padding: 11px;
    overflow: auto;
    white-space: pre;
    font-size: 12px;
    line-height: 1.5;
    background: #0b0e13;
    color: #d5dae3;
    border: 2px solid var(--line);
  }

  .upd-term .cmd {
    color: #d5dae3;
  }

  .upd-term .dim {
    color: #7f8898;
  }

  :global(.upd-selall-label) {
    display: none;
  }

  /* Phones: the panels are cards, the tables are rows (name and details, status at the right, progress under the row). */
  @media (max-width: 720px) {
    .upd-page {
      gap: 11px;
    }

    .upd-title {
      font-size: 22px;
    }

    :global(.upd-panel) {
      background: var(--surface);
      box-shadow: 3px 3px 0 var(--shade);
      gap: 9px;
    }

    :global(.upd-h) {
      font-size: 13px;
    }

    :global(.upd-cli > summary) {
      cursor: pointer;
      min-height: 29px;
      display: flex;
      align-items: center;
    }

    :global(.upd-cli[open] > summary) {
      margin-bottom: 9px;
    }

    :global(.upd-big) {
      font-size: 25px;
    }

    :global(.upd-kv) {
      font-size: 12px;
      gap: 0;
    }

    :global(.upd-kv dt) {
      padding-right: 13px;
    }

    :global(.upd-kv dt),
    :global(.upd-kv dd) {
      display: flex;
      align-items: center;
      min-height: 40px;
      border-top: 2px dashed var(--seg-off);
    }

    :global(.upd-kv dd) {
      justify-content: flex-end;
    }

    :global(.upd-kv dt:first-of-type),
    :global(.upd-kv dt:first-of-type + dd) {
      border-top: 0;
    }

    :global(.upd-acts .btn),
    :global(.upd-bar .btn) {
      flex: 1 1 100%;
    }

    :global(.upd-bar) {
      flex-direction: column;
      align-items: stretch;
      gap: 9px;
    }

    :global(.upd-label) {
      justify-content: space-between;
    }

    :global(.upd-check) {
      min-height: 44px;
      font-size: 12px;
    }

    :global(.upd-check input),
    :global(.upd-tbl input[type='checkbox']) {
      width: 20px;
      height: 20px;
    }

    /* The primary "update" button of the server stays in reach at the bottom of the sheet. */
    :global(.upd-pin) {
      position: fixed;
      left: 0;
      right: 0;
      bottom: 0;
      z-index: 3;
      padding: 9px 11px calc(9px + var(--safe-b));
      background: var(--surface);
      border-top: 2px solid var(--line);
    }

    :global(.upd-pin .btn) {
      width: 100%;
    }

    :global(.upd-page:has(.upd-pin)) {
      padding-bottom: calc(72px + var(--safe-b));
    }

    :global(.upd-tw) {
      overflow: visible;
    }

    :global(.upd-tbl),
    :global(.upd-tbl tbody) {
      display: block;
    }

    :global(.upd-tbl thead) {
      position: absolute;
      width: 1px;
      height: 1px;
      overflow: hidden;
      clip: rect(0 0 0 0);
    }

    /* The agents header keeps only the "select all" checkbox with its label. */
    :global(.upd-agents thead) {
      position: static;
      width: auto;
      height: auto;
      overflow: visible;
      clip: auto;
      display: block;
    }

    :global(.upd-agents thead tr) {
      display: flex;
      min-height: 44px;
      border-bottom: 2px solid var(--line);
    }

    :global(.upd-agents thead th:not(.upd-sel)) {
      display: none;
    }

    :global(.upd-agents thead th.upd-sel) {
      display: flex;
      align-items: center;
      gap: 9px;
      width: auto;
      border-bottom: 0;
      padding-left: 4px;
    }

    :global(.upd-selall-label) {
      display: inline;
      color: var(--muted);
      font-size: 11px;
      text-transform: none;
      letter-spacing: 0;
    }

    :global(.upd-tbl tbody tr) {
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      gap: 4px 9px;
      min-height: 52px;
      padding: 7px 4px;
      border-bottom: 2px dashed var(--seg-off);
    }

    :global(.upd-tbl tbody tr:last-child) {
      border-bottom: 0;
    }

    :global(.upd-tbl tbody td) {
      padding: 0;
      border: 0;
    }

    :global(.upd-tbl tbody td[colspan]) {
      flex: 1 0 100%;
    }

    /* Cache and history: a main line with the badge or button at the right, details on the second line. */
    :global(.upd-rows tbody tr) {
      display: grid;
      grid-template-columns: auto minmax(0, 1fr) auto;
      gap: 2px 9px;
    }

    :global(.upd-cache td:nth-child(1)) {
      grid-column: 1 / 3;
      grid-row: 1;
    }

    :global(.upd-cache td:nth-child(2)) {
      grid-column: 1;
      grid-row: 2;
    }

    :global(.upd-cache td:nth-child(3)) {
      grid-column: 2;
      grid-row: 2;
    }

    :global(.upd-cache td:nth-child(4)) {
      grid-column: 3;
      grid-row: 1 / span 2;
      justify-self: end;
    }

    :global(.upd-hist td:nth-child(3)) {
      grid-column: 1 / 3;
      grid-row: 1;
    }

    :global(.upd-hist td:nth-child(1)) {
      grid-column: 1;
      grid-row: 2;
    }

    :global(.upd-hist td:nth-child(2)) {
      grid-column: 2;
      grid-row: 2;
    }

    :global(.upd-hist td:nth-child(4)) {
      grid-column: 3;
      grid-row: 1 / span 2;
      justify-self: end;
    }

    :global(.upd-rows td[colspan]) {
      grid-column: 1 / -1;
      grid-row: auto;
    }

    /* Agents: a grid. Server and version at the left, status and button at the right, progress under the row. */
    :global(.upd-agents tbody tr) {
      display: grid;
      grid-template-columns: auto minmax(0, 1fr) auto;
      align-items: center;
      gap: 2px 9px;
    }

    :global(.upd-agents tbody td.upd-sel) {
      grid-column: 1;
      grid-row: 1 / span 2;
      width: auto;
    }

    :global(.upd-agents tbody td.upd-sel:empty) {
      display: none;
    }

    :global(.upd-agents tbody td:nth-child(2)) {
      grid-column: 2;
      grid-row: 1;
      min-width: 0;
    }

    :global(.upd-agents tbody td:nth-child(3)) {
      grid-column: 2;
      grid-row: 2;
      color: var(--muted);
      font-size: 10px;
    }

    :global(.upd-agents tbody td:nth-child(3))::before {
      content: attr(data-label) ' ';
    }

    :global(.upd-agents tbody td:nth-child(4)) {
      grid-column: 3;
      grid-row: 1;
      justify-self: end;
      text-align: right;
    }

    :global(.upd-agents tbody td:nth-child(5)) {
      grid-column: 1 / -1;
      grid-row: 3;
    }

    :global(.upd-agents tbody td:nth-child(5):has(.upd-dash)) {
      display: none;
    }

    :global(.upd-agents tbody td.upd-r) {
      grid-column: 3;
      grid-row: 2;
      justify-self: end;
    }

    :global(.upd-agents tbody td[colspan]) {
      grid-column: 1 / -1;
    }

    :global(.upd-pb) {
      width: 100%;
      height: 11px;
      margin: 2px 0 0;
    }
  }
</style>
