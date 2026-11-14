<script lang="ts">
  import { untrack } from 'svelte';
  import { api, type AptList, type ServerDetail } from '../../../api';
  import { locale, tr as tStore } from '../../../i18n';
  import Modal from '../../Modal.svelte';
  import { aptRun, startAptRun, type AptMode } from './apt';
  import { unavailableKind } from './svc';
  import { formatDateTime } from '../format';

  let { server }: { server: ServerDetail } = $props();

  let apt = $state<AptList | null>(null);
  let loadState = $state<'loading' | 'ready' | 'offline' | 'disabled' | 'error'>('loading');
  let confirmMode = $state<'security' | 'upgrade' | null>(null);
  let logEl = $state<HTMLPreElement | undefined>(undefined);

  let reqSeq = 0;
  let handledRun = 0;

  async function load(id: string) {
    const req = ++reqSeq;
    try {
      const data = await api.apt(id);
      if (req !== reqSeq) return;
      apt = data;
      loadState = 'ready';
    } catch (e) {
      if (req !== reqSeq) return;
      loadState = unavailableKind(e) ?? 'error';
    }
  }

  $effect(() => {
    const id = server.id;
    untrack(() => {
      apt = null;
      loadState = 'loading';
      void load(id);
    });
  });

  // The run belongs to the store, so only show it for this server.
  const run = $derived($aptRun && $aptRun.serverId === server.id ? $aptRun : null);
  const running = $derived(run?.active ?? false);
  const unreachable = $derived(!server.online || loadState === 'offline' || loadState === 'disabled');
  const securityCount = $derived(apt ? apt.packages.filter((p) => p.security).length : 0);
  const total = $derived(apt?.packages.length ?? 0);

  // Reload the package list once a run has finished.
  $effect(() => {
    const finished = run && !run.active ? run.id : 0;
    if (finished && finished !== handledRun) {
      handledRun = finished;
      untrack(() => void load(server.id));
    }
  });

  // Keep the log scrolled to the newest output.
  $effect(() => {
    void run?.text;
    if (logEl) logEl.scrollTop = logEl.scrollHeight;
  });

  function start(mode: AptMode) {
    if (mode === 'update') {
      startAptRun(server.id, mode);
    } else {
      confirmMode = mode;
    }
  }

  function confirmInstall() {
    const mode = confirmMode;
    confirmMode = null;
    if (mode) startAptRun(server.id, mode);
  }

  function closeLog() {
    if (!running) aptRun.set(null);
  }
</script>

<div class="apt">
  {#if loadState === 'offline'}
    <p class="notice" role="status">{$tStore('svc.offline')}</p>
  {:else if loadState === 'disabled'}
    <p class="notice" role="status">{$tStore('svc.disabled')}</p>
  {/if}

  <div class="head">
    <div class="summary">
      {#if apt}
        <p class="big">{total}</p>
        <p class="muted small">
          {$tStore('apt.summary', { n: total, s: securityCount })}
          {#if apt.last_check > 0}
            · {$tStore('apt.checked', { date: formatDateTime(apt.last_check, $locale) })}
          {:else}
            · {$tStore('apt.never')}
          {/if}
        </p>
      {:else if loadState === 'loading'}
        <p class="muted">{$tStore('common.loading')}</p>
      {:else if loadState === 'error'}
        <p class="error" role="alert">{$tStore('apt.loadFailed')}</p>
      {/if}
    </div>

    <div class="btns">
      <button class="btn" type="button" disabled={unreachable || running} onclick={() => start('update')}>
        {$tStore('apt.check')}
      </button>
      <button class="btn" type="button" disabled={unreachable || running || !apt} onclick={() => start('security')}>
        {$tStore('apt.installSecurity')}
      </button>
      <button class="btn primary" type="button" disabled={unreachable || running || !apt} onclick={() => start('upgrade')}>
        {$tStore('apt.installAll')}
      </button>
    </div>
  </div>

  {#if apt?.reboot_required}
    <p class="banner" role="status">{$tStore('apt.rebootNotice')}</p>
  {/if}

  {#if apt}
    {#if apt.packages.length === 0}
      <p class="muted">{$tStore('apt.empty')}</p>
    {:else}
      <div class="table-wrap">
        <table class="tbl">
          <thead>
            <tr>
              <th scope="col">{$tStore('apt.col.name')}</th>
              <th scope="col">{$tStore('apt.col.version')}</th>
              <th scope="col">{$tStore('apt.col.notes')}</th>
            </tr>
          </thead>
          <tbody>
            {#each apt.packages as pkg (pkg.name)}
              <tr>
                <td class="mono name">{pkg.name}</td>
                <td class="mono small version">{pkg.current} → {pkg.candidate}</td>
                <td>
                  <span class="notes">
                    {#if pkg.security}<span class="badge security">{$tStore('apt.badge.security')}</span>{/if}
                    {#if pkg.reboot}<span class="badge reboot">{$tStore('apt.badge.reboot')}</span>{/if}
                  </span>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {/if}
  {/if}

  {#if run}
    <section class="log-panel" aria-label={$tStore('apt.log')}>
      <header class="log-head">
        <span class="mono small">
          {$tStore('apt.log')} · {running ? $tStore('apt.running') : run.ok ? $tStore('apt.finished') : $tStore('apt.failed')}
        </span>
        <button class="btn small-btn" type="button" disabled={running} onclick={closeLog}>{$tStore('common.close')}</button>
      </header>
      <pre class="log" bind:this={logEl}>{run.text}</pre>
    </section>
  {/if}
</div>

{#if confirmMode}
  {@const mode = confirmMode}
  <Modal title={$tStore('apt.confirm.title')} labelId="apt-confirm-title" onclose={() => (confirmMode = null)}>
    {#snippet children()}
      <p class="confirm-text">
        {mode === 'security'
          ? $tStore('apt.confirm.security', { n: securityCount })
          : $tStore('apt.confirm.all', { n: total })}
      </p>
      <div class="row end">
        <button class="btn" type="button" onclick={() => (confirmMode = null)}>{$tStore('common.cancel')}</button>
        <button class="btn primary" type="button" onclick={confirmInstall}>{$tStore('apt.confirm.ok')}</button>
      </div>
    {/snippet}
  </Modal>
{/if}

<style>
  .apt {
    display: flex;
    flex-direction: column;
    gap: 12px;
    min-width: 0;
  }

  .head {
    display: flex;
    flex-wrap: wrap;
    align-items: flex-start;
    justify-content: space-between;
    gap: 12px;
  }

  .summary {
    min-width: 0;
  }

  .big {
    margin: 0;
    font-family: var(--font-display);
    font-size: 36px;
    line-height: 1;
  }

  .summary .small {
    margin: 4px 0 0;
  }

  .btns {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
  }

  .notice,
  .banner {
    margin: 0;
    padding: 8px 12px;
    border: 2px solid var(--warn);
    color: var(--warn);
  }

  .table-wrap {
    overflow-x: auto;
  }

  .tbl {
    width: 100%;
    min-width: 520px;
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

  .name {
    overflow-wrap: anywhere;
  }

  .version {
    color: var(--muted);
    white-space: nowrap;
  }

  .notes {
    display: inline-flex;
    flex-wrap: wrap;
    gap: 6px;
  }

  .badge {
    display: inline-block;
    padding: 1px 6px;
    border: 2px solid currentColor;
    font-size: 12px;
    white-space: nowrap;
  }

  .badge.security {
    color: var(--crit);
  }

  .badge.reboot {
    color: var(--warn);
  }

  .log-panel {
    display: flex;
    flex-direction: column;
    border: 2px solid var(--line);
    box-shadow: 3px 3px 0 var(--shade);
    min-width: 0;
  }

  .log-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    padding: 6px 10px;
    background: var(--ink);
    color: var(--surface);
  }

  .small-btn {
    padding: 2px 8px;
    font-size: 13px;
    background: var(--surface);
  }

  .log {
    margin: 0;
    padding: 10px;
    max-height: 360px;
    overflow: auto;
    background: var(--ink);
    color: var(--surface);
    font-family: var(--font-body);
    font-size: 12px;
    line-height: 1.5;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }

  .confirm-text {
    margin: 0;
  }
</style>
