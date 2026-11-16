<script lang="ts">
  import { onMount } from 'svelte';
  import { api, type AgentUpdateRow, type ServerDetail } from '../../api';
  import { tr } from '../../i18n';
  import Modal from '../Modal.svelte';
  import { showToast } from '../toast';
  import { errorText } from '../updates/format';

  let { server, onchanged }: { server: ServerDetail; onchanged?: () => void } = $props();

  const FAST_MS = 2000;
  const SLOW_MS = 15000;
  const TOTAL_STEPS = 5;

  let row = $state<AgentUpdateRow | null>(null);
  let confirmOpen = $state(false);
  let submitting = $state(false);

  const busy = $derived(row?.status === 'updating' || row?.status === 'queued');
  const canUpdate = $derived(
    server.online && (row?.status === 'available' || row?.status === 'unsupported'),
  );
  const target = $derived(row?.target ?? '');

  let timer: ReturnType<typeof setTimeout> | undefined;
  let destroyed = false;
  let inflight = false;
  let lastStatus: AgentUpdateRow['status'] | null = null;

  /** Stores the row of this server and reports how an update that was in progress ended. */
  function apply(rows: AgentUpdateRow[]): void {
    const next = rows.find((r) => r.id === server.id) ?? null;
    const wasBusy = lastStatus === 'updating' || lastStatus === 'queued';
    if (next && wasBusy) {
      if (next.status === 'current') {
        server.agent_version = next.version;
        showToast($tr('srv.agent.done', { name: server.name, version: next.version }));
        onchanged?.();
      } else if (next.status === 'rollback' || next.status === 'failed') {
        showToast($tr('srv.agent.rolledBack', { name: server.name }));
        onchanged?.();
      }
    }
    lastStatus = next?.status ?? null;
    row = next;
  }

  async function refresh(): Promise<void> {
    if (inflight || destroyed) return;
    inflight = true;
    try {
      apply((await api.updates()).agents);
    } catch {
      // optional block: keep the previous state and try again on the next tick
    } finally {
      inflight = false;
    }
  }

  function scheduleNext(): void {
    clearTimeout(timer);
    if (destroyed) return;
    timer = setTimeout(() => void tick(), busy ? FAST_MS : SLOW_MS);
  }

  async function tick(): Promise<void> {
    if (!document.hidden) await refresh();
    scheduleNext();
  }

  onMount(() => {
    void tick();
    return () => {
      destroyed = true;
      clearTimeout(timer);
    };
  });

  async function start(): Promise<void> {
    submitting = true;
    try {
      apply((await api.updatesAgents([server.id], 'all', true)).agents);
      confirmOpen = false;
      clearTimeout(timer);
      void tick();
    } catch (error) {
      showToast($tr('upd.err.request', { error: errorText(error) }));
    } finally {
      submitting = false;
    }
  }
</script>

{#if row && (canUpdate || busy || row.status === 'rollback' || row.status === 'failed')}
  <div class="banner" role="status">
    {#if row.status === 'available' || row.status === 'unsupported'}
      <span class="msg">
        {row.status === 'unsupported'
          ? $tr('srv.agent.unsupported', { version: row.version })
          : $tr('srv.agent.available', { version: target || row.version })}
      </span>
      <button class="btn sm" type="button" disabled={!canUpdate || submitting} onclick={() => (confirmOpen = true)}>
        {$tr('srv.agent.update')}
      </button>
    {:else if row.status === 'updating'}
      <span class="msg">
        {$tr('upd.ag.updating')}:
        {$tr('upd.ag.step', {
          n: row.step ?? 0,
          total: row.steps_total || TOTAL_STEPS,
          name: row.step_id ? $tr(`upd.agstep.${row.step_id}`) : '',
        })}{#if row.message} · {row.message}{/if}
      </span>
    {:else if row.status === 'queued'}
      <span class="msg">{$tr('upd.ag.queued')}{#if row.message}: {row.message}{:else if !row.online}: {$tr('upd.ag.waiting')}{/if}</span>
    {:else}
      <span class="msg">
        {row.status === 'rollback' ? $tr('upd.ag.rollback') : $tr('upd.ag.failed')}{#if row.message}: {row.message}{/if}
      </span>
      {#if canUpdate || server.online}
        <button class="btn sm" type="button" disabled={submitting} onclick={() => (confirmOpen = true)}>
          {$tr('srv.agent.update')}
        </button>
      {/if}
    {/if}
  </div>
{/if}

{#if confirmOpen}
  <Modal
    title={$tr('srv.agent.confirmTitle', { name: server.name })}
    labelId="agent-update-title"
    onclose={() => !submitting && (confirmOpen = false)}>
    {#snippet children()}
      <p class="text">{$tr('srv.agent.confirmText')}</p>
      <div class="row end">
        <button class="btn" type="button" disabled={submitting} onclick={() => (confirmOpen = false)}>
          {$tr('common.cancel')}
        </button>
        <button class="btn primary" type="button" disabled={submitting} onclick={start}>
          {$tr('srv.agent.confirmOk')}
        </button>
      </div>
    {/snippet}
  </Modal>
{/if}

<style>
  /* Same warning frame as the reboot banner, with the action button on the right. */
  .banner {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    margin: 0;
    padding: 8px 12px;
    border: 2px solid var(--warn);
    color: var(--warn);
  }

  .msg {
    color: var(--ink);
  }

  .text {
    margin: 0;
  }

  /* Phones: a card with the button across its full width. */
  @media (max-width: 720px) {
    .banner {
      background: var(--surface);
      box-shadow: 3px 3px 0 var(--shade);
    }

    .msg {
      flex: 1 1 100%;
    }

    .banner .btn {
      flex: 1 1 100%;
    }
  }
</style>
