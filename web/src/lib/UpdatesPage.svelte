<script lang="ts">
  import { onMount } from 'svelte';
  import { get } from 'svelte/store';
  import { api, type RolloutMode, type UpdatesInfo } from '../api';
  import { locale, t, tr as trans } from '../i18n';
  import { showToast } from './toast';
  import ServerPanel from './updates/ServerPanel.svelte';
  import CachePanel from './updates/CachePanel.svelte';
  import AgentsPanel from './updates/AgentsPanel.svelte';
  import HistoryPanel from './updates/HistoryPanel.svelte';
  import { errorText } from './updates/format';

  let { onBack, onchanged }: { onBack: () => void; onchanged?: () => void } = $props();

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
      <button class="btn sm" type="button" onclick={onBack}>{$trans('upd.back')}</button>
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
      <CachePanel cache={info.cache} {prefetching} onprefetch={prefetch} />
    </div>

    <AgentsPanel {info} onrun={runAgents} />

    <div class="upd-grid">
      <HistoryPanel history={info.history} />
      <section class="upd-panel">
        <h3 class="upd-h">{$trans('upd.cli.title')}</h3>
        <pre class="upd-term"><code><span class="cmd">linager update --check</span>      <span class="dim">{$trans('upd.cli.check')}</span>
<span class="cmd">linager update</span>              <span class="dim">{$trans('upd.cli.update')}</span>
<span class="cmd">linager agent update</span>        <span class="dim">{$trans('upd.cli.agentUpdate')}</span>
<span class="cmd">linager agent status</span>        <span class="dim">{$trans('upd.cli.agentStatus')}</span></code></pre>
      </section>
    </div>
  {/if}
</div>

<style>
  /* Shared building blocks for the updates page. Global because the panels are separate components. */
  :global(.upd-page) {
    display: flex;
    flex-direction: column;
    gap: 16px;
  }

  .upd-head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
  }

  .upd-ttl {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 12px;
  }

  .upd-title {
    margin: 0;
    font-variation-settings: "ELSH" 40;
    font-family: var(--font-display);
    font-size: 30px;
    font-weight: 400;
    line-height: 1;
  }

  :global(.upd-grid) {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
    gap: 16px;
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
    gap: 12px;
    min-width: 0;
    padding: 12px;
    border: 2px solid var(--line);
    background: var(--bg);
  }

  :global(.upd-h) {
    margin: 0;
    font-family: var(--font-display);
    font-size: 13px;
    font-weight: 400;
    text-transform: uppercase;
    letter-spacing: 0.08em;
    color: var(--muted);
  }

  :global(.upd-big) {
    font-variation-settings: "ELSH" 40;
    font-family: var(--font-display);
    font-size: 26px;
    line-height: 1.1;
    font-variant-numeric: tabular-nums;
  }

  :global(.upd-vers) {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 10px;
  }

  :global(.upd-kv) {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr);
    gap: 6px 14px;
    margin: 0;
    font-size: 12px;
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
    padding: 5px 8px;
    min-width: 0;
    font: inherit;
    font-size: 13px;
  }

  :global(.upd-select:disabled) {
    opacity: 0.5;
  }

  :global(.upd-list) {
    margin: 0;
    padding-left: 18px;
    font-size: 12px;
    display: flex;
    flex-direction: column;
    gap: 3px;
  }

  :global(.upd-acts) {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
  }

  :global(.upd-acts-end) {
    justify-content: flex-end;
    margin-top: 8px;
  }

  :global(.upd-bar) {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 10px 14px;
  }

  :global(.upd-label),
  :global(.upd-check) {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    font-size: 12px;
    color: var(--muted);
  }

  :global(.upd-check input),
  :global(.upd-tbl input[type='checkbox']) {
    width: 16px;
    height: 16px;
    margin: 0;
    accent-color: var(--accent);
  }

  :global(.upd-intro) {
    margin: 0;
    font-size: 12px;
    color: var(--muted);
  }

  :global(.upd-tw) {
    overflow-x: auto;
  }

  :global(.upd-tbl) {
    width: 100%;
    border-collapse: collapse;
    font-size: 12px;
  }

  :global(.upd-tbl th) {
    font-family: var(--font-display);
    font-size: 12px;
    font-weight: 400;
    text-transform: uppercase;
    letter-spacing: 0.08em;
    color: var(--muted);
    text-align: left;
    padding: 6px 8px;
    border-bottom: 2px solid var(--line);
    white-space: nowrap;
  }

  :global(.upd-tbl td) {
    padding: 7px 8px;
    border-bottom: 2px dashed var(--seg-off);
    vertical-align: middle;
  }

  :global(.upd-tbl .upd-sel) {
    width: 36px;
  }

  :global(.upd-tbl .upd-r) {
    text-align: right;
    white-space: nowrap;
  }

  :global(.upd-agents td) {
    padding-top: 10px;
    padding-bottom: 10px;
  }

  :global(.upd-name) {
    font-variation-settings: "ELSH" 40;
    font-family: var(--font-display);
    font-size: 15px;
  }

  :global(.upd-sub) {
    color: var(--muted);
    font-size: 12px;
  }

  :global(.upd-mono) {
    font-variant-numeric: tabular-nums;
    overflow-wrap: anywhere;
  }

  :global(.upd-badge) {
    display: inline-block;
    border: 2px solid var(--muted);
    color: var(--muted);
    padding: 0 6px;
    font-size: 11px;
    line-height: 18px;
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
    font-size: 12px;
    overflow-wrap: anywhere;
  }

  :global(.upd-ok) {
    margin: 0;
    color: var(--good);
    font-size: 12px;
  }

  :global(.upd-hint) {
    margin: 0;
    color: var(--muted);
    font-size: 12px;
  }

  :global(.upd-modal-text) {
    margin: 0;
  }

  :global(.upd-run) {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 10px;
    border: 2px dashed var(--seg-off);
  }

  :global(.upd-steps) {
    margin: 0;
    padding: 0;
    list-style: none;
    display: flex;
    flex-direction: column;
    gap: 4px;
    font-size: 12px;
  }

  :global(.upd-step) {
    display: flex;
    align-items: center;
    gap: 8px;
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

  :global(.upd-ico) {
    display: inline-flex;
    justify-content: center;
    width: 14px;
  }

  :global(.upd-spin) {
    display: inline-block;
    width: 10px;
    height: 10px;
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
    height: 10px;
    width: 110px;
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
    padding: 12px;
    overflow: auto;
    white-space: pre;
    font-size: 13px;
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
</style>
