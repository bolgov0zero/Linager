<script lang="ts">
  import type { UpdateStep, UpdatesInfo } from '../../api';
  import Select from '../ui/Select.svelte';
  import Modal from '../Modal.svelte';
  import { isPhone } from '../ui/viewport';
  import { locale, tr as trans } from '../../i18n';
  import { RUN_VISIBLE_SEC, SERVER_STEPS, fmtDateTime } from './format';

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
  const showRun = $derived(
    run.state === 'running' ||
      (run.state !== 'idle' && run.finished_at > 0 && Date.now() / 1000 - run.finished_at < RUN_VISIBLE_SEC),
  );

  function stepState(id: UpdateStep['id']): UpdateStep['state'] {
    return run.steps?.find((step) => step.id === id)?.state ?? 'pending';
  }

  const notes = $derived(srv.notes ?? []);
  const notesVersion = $derived(srv.notes_version ?? available ?? srv.current);

  // After a check that found a newer release, its description opens by itself.
  let wasChecking = false;
  $effect(() => {
    const now = checking;
    if (wasChecking && !now && available && notes.length > 0) notesOpen = true;
    wasChecking = now;
  });

  function updateFromNotes(): void {
    notesOpen = false;
    confirmOpen = true;
  }

  function confirmUpdate(): void {
    confirmOpen = false;
    if (available) onupdate(available);
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

  {#if restarting || showRun}
    <div class="upd-run" aria-label={$trans('upd.srv.progressAria')}>
      {#if restarting}
        <p class="upd-hint">{$trans('upd.restarting')}</p>
      {/if}
      {#if showRun}
        <ol class="upd-steps">
          {#each SERVER_STEPS as id (id)}
            {@const st = stepState(id)}
            <li class="upd-step {st}">
              <span class="upd-ico" aria-hidden="true">
                {#if st === 'done'}✓{:else if st === 'running'}<span class="upd-spin"></span>{:else if st === 'failed'}✗{:else}○{/if}
              </span>
              <span>{$trans(`upd.srvstep.${id}`)}</span>
            </li>
          {/each}
        </ol>
        {#if run.state === 'done'}
          <p class="upd-ok">{$trans('upd.run.done', { version: run.to })}</p>
        {:else if run.state === 'failed'}
          <p class="upd-err">{$trans('upd.run.failed', { error: run.error ?? '' })}</p>
        {:else if run.state === 'rolled_back'}
          <p class="upd-err">{$trans('upd.run.rolledBack', { error: run.error ?? '' })}</p>
        {/if}
      {/if}
    </div>
  {/if}
</section>

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
      {#if canUpdate && available}
        <button class="btn primary" type="button" disabled={busy} onclick={updateFromNotes}>
          {$trans('upd.srv.update', { version: available })}
        </button>
      {/if}
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
