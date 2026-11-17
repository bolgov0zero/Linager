<script lang="ts">
  import type { UpdateStep, UpdatesInfo } from '../../api';
  import Select from '../ui/Select.svelte';
  import Modal from '../Modal.svelte';
  import { isPhone } from '../ui/viewport';
  import { locale, tr as trans } from '../../i18n';
  import { SERVER_STEPS, fmtDateTime, fmtDuration } from './format';

  let {
    info,
    busy,
    restarting,
    checking,
    channelBusy,
    oncheck,
    onchannel,
    onupdate,
  }: {
    info: UpdatesInfo;
    /** Server update running or being started. */
    busy: boolean;
    restarting: boolean;
    checking: boolean;
    channelBusy: boolean;
    oncheck: () => void;
    onchannel: (channel: 'stable' | 'beta') => void;
    onupdate: (version: string) => void;
  } = $props();

  let confirmOpen = $state(false);
  let notesOpen = $state(false);

  const srv = $derived(info.server);
  const run = $derived(srv.update_run);
  const available = $derived(srv.available);
  const canUpdate = $derived(Boolean(available) && srv.can_update);
  const noUpdater = $derived(Boolean(available) && !srv.can_update);

  // The progress of a server update is a dialog: it opens when the update is confirmed (or when a run is seen going on),
  // and after a good result it closes by itself in 5 seconds or by the OK button.
  const CLOSE_AFTER_MS = 5000;
  let statusOpen = $state(false);
  /** The run that was the latest before the update was confirmed: its state is not the one of the new update. */
  let staleRun = $state('');
  /** The run whose dialog the administrator closed; it is not opened again. */
  let dismissedRun = $state('');
  const freshRun = $derived(run.run_id !== staleRun);
  const finished = $derived(freshRun && (run.state === 'done' || run.state === 'failed' || run.state === 'rolled_back'));
  const working = $derived(!finished);

  $effect(() => {
    if ((run.state === 'running' || restarting) && run.run_id !== dismissedRun) statusOpen = true;
  });

  $effect(() => {
    if (!statusOpen || !finished || run.state !== 'done') return;
    const timer = setTimeout(closeStatus, CLOSE_AFTER_MS);
    return () => clearTimeout(timer);
  });

  // The update did not start (the request failed): nothing to show.
  $effect(() => {
    if (statusOpen && !busy && !restarting && !freshRun) statusOpen = false;
  });

  function closeStatus(): void {
    statusOpen = false;
    dismissedRun = run.run_id;
  }

  function stepState(id: UpdateStep['id']): UpdateStep['state'] {
    if (!freshRun) return 'pending';
    return run.steps?.find((step) => step.id === id)?.state ?? 'pending';
  }

  // The panel is served by the service that the update stops for a few seconds, so for that time nothing can tell which
  // step is going on. The dialog says so instead of standing still on the last step it saw: the steps that were not
  // finished at that moment are shown as "going on", and the real result (with the time of every step) appears when the
  // service is back.
  let now = $state(Date.now());
  let restartSince = $state(0);
  $effect(() => {
    if (!restarting) {
      restartSince = 0;
      return;
    }
    restartSince = now = Date.now();
    const timer = setInterval(() => (now = Date.now()), 1000);
    return () => clearInterval(timer);
  });
  const waiting = $derived(restarting && freshRun && !finished);
  const waitedSec = $derived(restartSince ? Math.max(0, Math.round((now - restartSince) / 1000)) : 0);

  function shownState(id: UpdateStep['id']): UpdateStep['state'] | 'unknown' {
    const st = stepState(id);
    return waiting && st !== 'done' && st !== 'failed' ? 'unknown' : st;
  }

  /** How long a finished step took ("" while it has not finished or the times are not known). */
  function took(id: UpdateStep['id']): string {
    const step = freshRun ? run.steps?.find((s) => s.id === id) : undefined;
    if (!step || (step.state !== 'done' && step.state !== 'failed') || !step.started_ms || !step.finished_ms) return '';
    return fmtDuration(step.finished_ms - step.started_ms, $trans, $locale);
  }

  const notes = $derived(srv.notes ?? []);
  const notesVersion = $derived(srv.notes_version ?? available ?? srv.current);

  function confirmUpdate(): void {
    confirmOpen = false;
    if (!available) return;
    staleRun = run.run_id;
    dismissedRun = '';
    statusOpen = true;
    onupdate(available);
  }
</script>

<section class="upd-panel">
  <h3 class="upd-h">{$trans('upd.srv.title')}</h3>

  <div class="upd-vers">
    <span class="upd-big">{srv.current}</span>
    {#if available}
      <span class="upd-badge warn">{$trans('upd.srv.available', { version: available })}</span>
    {:else}
      <span class="upd-badge ok">{$trans('upd.srv.current')}</span>
    {/if}
  </div>

  <dl class="upd-kv">
    <dt>{$trans('upd.srv.channel')}</dt>
    <dd>
      <Select
        size="sm"
        ariaLabel={$trans('upd.srv.channelAria')}
        value={info.channel as 'stable' | 'beta'}
        disabled={busy || channelBusy}
        onchange={onchannel}
        options={[
          { value: 'stable', label: 'stable' },
          { value: 'beta', label: 'beta' },
        ]}
      />
    </dd>
    <dt>{$trans('upd.srv.checked')}</dt>
    <dd>{fmtDateTime(srv.checked_at, $trans, $locale)}</dd>
    <dt>{$trans('upd.srv.migrations')}</dt>
    <dd>{srv.db_migrations.pending > 0 ? $trans('upd.srv.migrationsPending') : $trans('upd.srv.migrationsOk')}</dd>
    <dt>{$trans('upd.srv.backup')}</dt>
    <dd>{srv.last_backup ? fmtDateTime(srv.last_backup, $trans, $locale) : '—'}</dd>
    <dt>{$trans('upd.srv.source')}</dt>
    <dd class="upd-mono">{srv.source}</dd>
  </dl>

  {#if srv.check_error}
    <p class="upd-err">{srv.check_error}</p>
  {/if}

  <div class="upd-acts">
    <button class="btn sm" type="button" disabled={busy || checking} onclick={oncheck}>
      {checking ? $trans('upd.srv.checking') : $trans('upd.srv.check')}
    </button>
    <button class="btn sm" type="button" disabled={notes.length === 0} onclick={() => (notesOpen = true)}>
      {$trans('upd.notes.button')}
    </button>
    {#if canUpdate && !$isPhone}
      <button class="btn sm primary" type="button" disabled={busy} onclick={() => (confirmOpen = true)}>
        {$trans('upd.srv.update', { version: available ?? '' })}
      </button>
    {/if}
  </div>
  {#if canUpdate && $isPhone}
    <div class="upd-pin">
      <button class="btn primary" type="button" disabled={busy} onclick={() => (confirmOpen = true)}>
        {$trans('upd.srv.update', { version: available ?? '' })}
      </button>
    </div>
  {/if}
  {#if noUpdater}
    <p class="upd-hint">{$trans('upd.srv.noUpdater')}</p>
  {/if}
</section>

{#if statusOpen}
  <Modal title={$trans('upd.status.title')} labelId="upd-status-title" onclose={closeStatus}>
    <div class="upd-run" aria-label={$trans('upd.srv.progressAria')}>
      {#if waiting}
        <p class="upd-hint" role="status">{$trans('upd.restarting')} {$trans('upd.restart.elapsed', { n: waitedSec })}</p>
        <p class="upd-hint">{$trans('upd.restart.note')}</p>
      {:else if restarting}
        <p class="upd-hint">{$trans('upd.restarting')}</p>
      {/if}
      <ol class="upd-steps">
        {#each SERVER_STEPS as id (id)}
          {@const st = shownState(id)}
          {@const dur = took(id)}
          <li class="upd-step {st}">
            <span class="upd-ico" aria-hidden="true">
              {#if st === 'done'}✓{:else if st === 'running'}<span class="upd-spin"></span>{:else if st === 'unknown'}<span class="upd-dots">…</span>{:else if st === 'failed'}✗{:else}○{/if}
            </span>
            <span>{$trans(`upd.srvstep.${id}`)}</span>
            {#if dur}<span class="upd-dur">{dur}</span>{/if}
          </li>
        {/each}
      </ol>
      {#if finished && run.state === 'done'}
        <p class="upd-ok">{$trans('upd.run.done', { version: run.to })}</p>
      {:else if finished && run.state === 'failed'}
        <p class="upd-err">{$trans('upd.run.failed', { error: run.error ?? '' })}</p>
      {:else if finished && run.state === 'rolled_back'}
        <p class="upd-err">{$trans('upd.run.rolledBack', { error: run.error ?? '' })}</p>
      {/if}
    </div>
    <div class="upd-acts upd-acts-end row end">
      <button class="btn primary" type="button" disabled={working} onclick={closeStatus}>{$trans('upd.status.ok')}</button>
    </div>
  </Modal>
{/if}

{#if notesOpen}
  <Modal title={$trans('upd.notes.title', { version: notesVersion })} labelId="upd-notes-title" onclose={() => (notesOpen = false)}>
    {#if notes.length > 0}
      <ul class="upd-list">
        {#each notes as note, i (i)}
          <li>{note}</li>
        {/each}
      </ul>
    {:else}
      <p class="upd-modal-text">{$trans('upd.notes.empty')}</p>
    {/if}
    <div class="upd-acts upd-acts-end row end">
      <button class="btn" type="button" onclick={() => (notesOpen = false)}>{$trans('upd.notes.close')}</button>
    </div>
  </Modal>
{/if}

{#if confirmOpen && available}
  <Modal title={$trans('upd.confirm.title', { version: available })} labelId="upd-confirm-title" onclose={() => (confirmOpen = false)}>
    <p class="upd-modal-text">{$trans('upd.confirm.text1')}</p>
    <p class="upd-modal-text">{$trans('upd.confirm.text2')}</p>
    <p class="upd-modal-text">{$trans('upd.confirm.text3')}</p>
    <div class="upd-acts upd-acts-end row end">
      <button class="btn" type="button" onclick={() => (confirmOpen = false)}>{$trans('upd.cancel')}</button>
      <button class="btn primary" type="button" onclick={confirmUpdate}>{$trans('upd.confirm.ok')}</button>
    </div>
  </Modal>
{/if}
