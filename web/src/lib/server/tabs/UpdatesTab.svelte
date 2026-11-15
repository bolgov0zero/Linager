<script lang="ts">
  import { untrack } from 'svelte';
  import { get } from 'svelte/store';
  import { api, type AptList, type ServerDetail } from '../../../api';
  import { locale, tr as tStore } from '../../../i18n';
  import Modal from '../../Modal.svelte';
  import Segments from '../../ui/Segments.svelte';
  import AptConsole from './AptConsole.svelte';
  import { aptRun, startAptRun, type AptMode } from './apt';
  import { unavailableKind } from './svc';
  import { formatDateTime } from '../format';

  let { server }: { server: ServerDetail } = $props();

  let apt = $state<AptList | null>(null);
  let loadState = $state<'loading' | 'ready' | 'offline' | 'disabled' | 'error'>('loading');
  let confirmMode = $state<'security' | 'upgrade' | null>(null);
  let wrapEl = $state<HTMLDivElement | undefined>(undefined);
  /** The check run whose modal the user (or the timer) has already closed. */
  let dismissedId = $state(0);
  /** The finished run after which the list has been reloaded. */
  let reloadedRun = $state(0);

  let reqSeq = 0;
  let handledRun = 0;

  async function load(id: string) {
    const req = ++reqSeq;
    try {
      const data = await api.apt(id);
      if (req !== reqSeq) return;
      apt = data;
      loadState = 'ready';
      // the browser was closed in the middle of a run: attach to it, the agent replays its events
      if (data.busy && !get(aptRun)) startAptRun(id, 'upgrade');
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

  // An install run is shown in place of the package list, a check run in a modal.
  const installRun = $derived(run && run.mode !== 'update' ? run : null);
  const checkRun = $derived(run && run.mode === 'update' ? run : null);
  const doneCount = $derived(installRun ? installRun.pkgs.filter((p) => p.state === 'done').length : 0);
  const errCount = $derived(installRun ? installRun.pkgs.filter((p) => p.state === 'error').length : 0);

  const installLabel = $derived.by(() => {
    if (!installRun) return '';
    const pr = installRun.progress;
    if (installRun.active) {
      if (!pr) return $tStore('apt.run.preparing');
      return $tStore(pr.stage === 'download' ? 'apt.run.download' : 'apt.run.install', { pct: pr.pct });
    }
    if (installRun.pkgs.length === 0) return $tStore('apt.run.nothing');
    const vars = { done: doneCount, total: installRun.pkgs.length, err: errCount };
    return $tStore(errCount > 0 || !installRun.ok ? 'apt.run.finishedErr' : 'apt.run.finished', vars);
  });

  const checkLabel = $derived.by(() => {
    if (!checkRun) return '';
    if (checkRun.active) {
      const pr = checkRun.progress;
      if (pr && pr.total > 0) return $tStore('apt.checking.files', { cur: pr.cur, total: pr.total });
      if (checkRun.sources > 0) return $tStore('apt.checking.sources', { n: checkRun.sources });
      return $tStore('apt.checking.start');
    }
    if (!checkRun.ok) return $tStore('apt.checking.failed');
    return reloadedRun === checkRun.id && apt
      ? `${$tStore('apt.checkDone')} · ${$tStore('apt.summary', { n: total, s: securityCount })}`
      : $tStore('apt.checkDone');
  });
  const checkPct = $derived(!checkRun ? 0 : !checkRun.active ? 100 : (checkRun.progress?.total ?? 0) > 0 ? checkRun.progress!.pct : null);

  // Reload the package list once a run has finished.
  $effect(() => {
    const finished = run && !run.active ? run.id : 0;
    if (finished && finished !== handledRun) {
      handledRun = finished;
      untrack(() => void load(server.id).then(() => (reloadedRun = finished)));
    }
  });

  // A successful check closes its modal by itself, three seconds after the end. A failed one stays
  // open until it is closed; when it was closed already, the toast has said everything.
  $effect(() => {
    const r = checkRun;
    if (!r || r.active) return;
    const id = r.id;
    const closed = dismissedId === id;
    if (!r.ok && !closed) return;
    const timer = setTimeout(() => {
      if (get(aptRun)?.id === id) aptRun.set(null);
    }, closed ? 0 : 3000);
    return () => clearTimeout(timer);
  });

  // Keep the package being updated in view inside the table.
  $effect(() => {
    const current = installRun?.pkgs.find((p) => p.state === 'running')?.name;
    if (!current || !wrapEl) return;
    const row = wrapEl.querySelector<HTMLElement>('tr.r-running');
    if (!row) return;
    const offset = row.getBoundingClientRect().top - wrapEl.getBoundingClientRect().top + wrapEl.scrollTop;
    wrapEl.scrollTop = Math.max(0, offset - wrapEl.clientHeight / 2);
  });

  function start(mode: AptMode) {
    if (mode === 'update') {
      dismissedId = 0;
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

  function closeRun() {
    if (!running) aptRun.set(null);
  }

  function closeCheck() {
    if (!checkRun) return;
    dismissedId = checkRun.id;
    if (!checkRun.active) aptRun.set(null);
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

  {#if installRun}
    <section class="run" aria-label={$tStore('apt.log')}>
      <div class="run-head">
        <span class="run-label" role="status">{installLabel}</span>
        {#if installRun.pkgs.length > 0}
          <span class="muted small">{$tStore('apt.run.counts', { done: doneCount + errCount, total: installRun.pkgs.length })}</span>
        {/if}
      </div>
      <!-- apt's last percentage stays below 100 even when everything is done -->
      <Segments
        value={!installRun.active && installRun.ok && errCount === 0 ? 100 : (installRun.progress?.pct ?? 0)}
        plain
        busy={installRun.active && !installRun.progress}
      />
      {#if installRun.pkgs.length > 0}
        <div class="table-wrap run-wrap" bind:this={wrapEl}>
          <table class="tbl">
            <thead>
              <tr>
                <th scope="col">{$tStore('apt.col.name')}</th>
                <th scope="col">{$tStore('apt.col.version')}</th>
                <th scope="col">{$tStore('apt.col.status')}</th>
              </tr>
            </thead>
            <tbody>
              {#each installRun.pkgs as pkg (pkg.name)}
                <tr class="r-{pkg.state}">
                  <td class="mono name">{pkg.name}</td>
                  <td class="mono small version">
                    {#if pkg.candidate}{pkg.current} → {pkg.candidate}{/if}
                  </td>
                  <td>
                    <span class="st {pkg.state}" title={pkg.msg || undefined}>{$tStore('apt.st.' + pkg.state)}</span>
                  </td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {/if}
      <AptConsole text={installRun.text} />
      {#if !installRun.active}
        <div class="row end">
          <button class="btn" type="button" onclick={closeRun}>{$tStore('common.close')}</button>
        </div>
      {/if}
    </section>
  {:else if apt}
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
</div>

{#if checkRun && dismissedId !== checkRun.id}
  <Modal title={$tStore('apt.checking.title')} labelId="apt-check-title" onclose={closeCheck}>
    {#snippet children()}
      <div class="check">
        <p class="run-label" class:failed={!checkRun.active && !checkRun.ok} role="status">{checkLabel}</p>
        <Segments value={checkPct ?? 0} plain busy={checkPct === null} />
        <AptConsole text={checkRun.text} />
        {#if !checkRun.active && !checkRun.ok}
          <div class="row end">
            <button class="btn" type="button" onclick={closeCheck}>{$tStore('common.close')}</button>
          </div>
        {/if}
      </div>
    {/snippet}
  </Modal>
{/if}

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
    font-variation-settings: "ELSH" 40;
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

  .run,
  .check {
    display: flex;
    flex-direction: column;
    gap: 10px;
    min-width: 0;
  }

  .run-head {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 12px;
  }

  .run-label {
    margin: 0;
  }

  .run-label.failed {
    color: var(--crit);
  }

  .run-wrap {
    max-height: 420px;
    overflow-y: auto;
  }

  .tbl tr.r-running td {
    background: color-mix(in srgb, var(--accent) 10%, transparent);
  }

  .tbl tr.r-error .name {
    color: var(--crit);
  }

  .st {
    display: inline-block;
    padding: 1px 6px;
    border: 2px solid currentColor;
    font-size: 12px;
    white-space: nowrap;
  }

  .st.queued {
    color: var(--muted);
  }

  .st.running {
    color: var(--accent);
    animation: st-blink 1s steps(2) infinite;
  }

  .st.done {
    color: var(--good);
  }

  .st.error {
    color: var(--crit);
  }

  .st.skipped {
    color: var(--muted);
    border-style: dashed;
  }

  @keyframes st-blink {
    50% {
      opacity: 0.45;
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .st.running {
      animation: none;
    }
  }

  .confirm-text {
    margin: 0;
  }

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
      gap: 4px 12px;
      padding: 8px 4px;
      border-bottom: 1px dashed var(--seg-off);
    }

    .tbl td {
      padding: 0;
      border: 0;
    }

    .tbl td.name {
      flex: 1 0 100%;
    }
  }
</style>
