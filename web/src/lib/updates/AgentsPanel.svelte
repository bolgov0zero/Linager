<script lang="ts">
  import type { AgentUpdateRow, RolloutMode, UpdatesInfo } from '../../api';
  import Fold from '../ui/Fold.svelte';
  import AgentSteps from './AgentSteps.svelte';
  import Select from '../ui/Select.svelte';
  import { tr as trans } from '../../i18n';

  let {
    info,
    onrun,
  }: {
    info: UpdatesInfo;
    /** Resolves true when the request was accepted. */
    onrun: (ids: string[] | 'all', mode: RolloutMode, stopOnError: boolean) => Promise<boolean>;
  } = $props();

  const APPLICABLE = new Set<AgentUpdateRow['status']>(['available', 'unsupported', 'failed', 'rollback']);
  const TOTAL_STEPS = 8;

  let mode = $state<RolloutMode>('first_then_rest');
  let stopOnError = $state(true);
  /** Checkbox state by agent id. Kept apart from `info`, so refreshes never reset it. */
  let picked = $state<Record<string, boolean>>({});
  let submitting = $state(false);
  /** Agents whose list of stages is open. Kept apart from `info`, so refreshes never close it. */
  let opened = $state<Record<string, boolean>>({});

  const applicable = $derived(info.agents.filter((agent) => APPLICABLE.has(agent.status)));
  const selectedIds = $derived(applicable.filter((agent) => picked[agent.id]).map((agent) => agent.id));
  const rolloutRunning = $derived(info.rollout?.state === 'running');
  const canAct = $derived(!rolloutRunning && !submitting);
  const allSelected = $derived(applicable.length > 0 && selectedIds.length === applicable.length);
  const needServer = $derived(
    Boolean(info.server.available) && info.server.available !== info.server.current,
  );

  /** The folded header says what needs doing, so the list can stay closed. */
  const meta = $derived(
    rolloutRunning
      ? $trans('upd.agents.metaRunning')
      : applicable.length > 0
        ? $trans('upd.agents.metaNeed', { n: applicable.length })
        : info.agents.length > 0
          ? $trans('upd.agents.metaOk')
          : '',
  );
  const metaTone = $derived<'ok' | 'warn' | 'muted'>(rolloutRunning || applicable.length > 0 ? 'warn' : info.agents.length > 0 ? 'ok' : 'muted');

  async function run(ids: string[] | 'all', clear: string[] = []): Promise<void> {
    submitting = true;
    const ok = await onrun(ids, mode, stopOnError);
    submitting = false;
    if (ok && clear.length > 0) {
      const next = { ...picked };
      for (const id of clear) next[id] = false;
      picked = next;
    }
  }

  function toggleAll(on: boolean): void {
    const next = { ...picked };
    for (const agent of applicable) next[agent.id] = on;
    picked = next;
  }

  function stepName(agent: AgentUpdateRow): string {
    return agent.step_id ? $trans(`upd.agstep.${agent.step_id}`) : '';
  }

  function badgeClass(status: AgentUpdateRow['status']): string {
    switch (status) {
      case 'current':
        return 'ok';
      case 'available':
      case 'updating':
        return 'warn';
      case 'unsupported':
      case 'rollback':
      case 'failed':
        return 'crit';
      default:
        return 'muted';
    }
  }
</script>

{#snippet stages(agent: AgentUpdateRow)}
  <button
    class="more"
    type="button"
    aria-expanded={Boolean(opened[agent.id])}
    onclick={() => (opened = { ...opened, [agent.id]: !opened[agent.id] })}>
    {opened[agent.id] ? $trans('upd.agstep.hide') : $trans('upd.agstep.show')}
  </button>
  {#if opened[agent.id] && agent.steps}
    <div class="stages"><AgentSteps steps={agent.steps} nowMs={agent.now_ms} rollbackAt={agent.rollback_at} /></div>
  {/if}
{/snippet}

<Fold title={$trans('upd.agents.title')} {meta} tone={metaTone}>
  <p class="upd-intro">
    {$trans('upd.agents.intro')} <b class="upd-mono">{info.agents_target}</b>.
    {#if needServer}{$trans('upd.agents.needServer', { version: info.server.available ?? '' })}{/if}
  </p>

  {#if rolloutRunning}
    <p class="upd-hint">{$trans('upd.agents.rolloutRunning')}</p>
  {/if}

  <div class="upd-bar">
    <label class="upd-label">
      {$trans('upd.agents.order')}
      <Select
        size="sm"
        ariaLabel={$trans('upd.agents.orderAria')}
        bind:value={mode}
        disabled={!canAct}
        options={[
          { value: 'all' as RolloutMode, label: $trans('upd.mode.all') },
          { value: 'one_by_one' as RolloutMode, label: $trans('upd.mode.one') },
          { value: 'first_then_rest' as RolloutMode, label: $trans('upd.mode.first') },
        ]}
      />
    </label>
    <label class="upd-check">
      <input type="checkbox" bind:checked={stopOnError} />
      {$trans('upd.agents.stopOnError')}
    </label>
    <button
      class="btn sm"
      type="button"
      disabled={!canAct || selectedIds.length === 0}
      onclick={() => run(selectedIds, selectedIds)}
    >
      {$trans('upd.agents.runSelected', { count: selectedIds.length })}
    </button>
    <button
      class="btn sm primary"
      type="button"
      disabled={!canAct || applicable.length === 0}
      onclick={() => run('all')}
    >
      {$trans('upd.agents.runAll', { count: applicable.length })}
    </button>
  </div>

  <div class="upd-tw">
    <table class="upd-tbl upd-agents">
      <thead>
        <tr>
          <th class="upd-sel">
            <input
              type="checkbox"
              aria-label={$trans('upd.agents.selectAll')}
              checked={allSelected}
              disabled={!canAct || applicable.length === 0}
              onchange={(event) => toggleAll(event.currentTarget.checked)}
            />
            <span class="upd-selall-label">{$trans('upd.agents.selectAll')}</span>
          </th>
          <th>{$trans('upd.agents.colServer')}</th>
          <th>{$trans('upd.agents.colVersion')}</th>
          <th>{$trans('upd.agents.colStatus')}</th>
          <th>{$trans('upd.agents.colProgress')}</th>
          <th></th>
        </tr>
      </thead>
      <tbody>
        {#each info.agents as agent (agent.id)}
          {@const isApplicable = APPLICABLE.has(agent.status)}
          <tr>
            <td class="upd-sel">
              {#if isApplicable}
                <input
                  type="checkbox"
                  aria-label={$trans('upd.agents.select', { name: agent.name })}
                  bind:checked={picked[agent.id]}
                  disabled={!canAct}
                />
              {/if}
            </td>
            <td>
              <div class="upd-name">{agent.name}</div>
              <div class="upd-sub">{agent.os} · {agent.address}</div>
            </td>
            <td class="upd-mono" data-label={$trans('upd.agents.colVersion')}>{agent.version}</td>
            <td>
              {#if agent.status === 'current' && agent.manual}
                <span class="upd-badge warn">{$trans('upd.ag.manual', { version: agent.target ?? '' })}</span>
              {:else if agent.status === 'current'}
                <span class="upd-badge ok">{$trans('upd.ag.current')}</span>
              {:else if agent.status === 'available'}
                <span class="upd-badge warn">{$trans('upd.ag.available', { version: agent.target ?? info.server.available ?? '' })}</span>
              {:else if agent.status === 'unsupported'}
                <span class="upd-badge crit">{$trans('upd.ag.unsupported')}</span>
              {:else if agent.status === 'updating'}
                <span class="upd-badge warn">{$trans('upd.ag.updating')}</span>
                <div class="upd-sub">
                  {$trans('upd.ag.step', { n: agent.step ?? 0, total: agent.steps_total || TOTAL_STEPS, name: stepName(agent) })}
                </div>
              {:else if agent.status === 'queued'}
                <span class="upd-badge muted">{$trans('upd.ag.queued')}</span>
                {#if !agent.online}
                  <div class="upd-sub">{$trans('upd.ag.waiting')}</div>
                {/if}
              {:else if agent.status === 'rollback'}
                <span class="upd-badge crit">{$trans('upd.ag.rollback')}</span>
              {:else}
                <span class="upd-badge crit">{$trans('upd.ag.failed')}</span>
              {/if}
            </td>
            <td>
              {#if agent.status === 'updating'}
                <div class="upd-pb" aria-hidden="true">
                  {#each Array.from({ length: agent.steps_total || TOTAL_STEPS }) as _, i (i)}
                    <i class:on={i < (agent.step ?? 0)}></i>
                  {/each}
                </div>
                {#if agent.message}<div class="upd-sub">{agent.message}</div>{/if}
              {:else if agent.status === 'rollback' || agent.status === 'failed'}
                <span class="upd-err-inline">{agent.message ?? ''}</span>
                {#if agent.steps?.length}
                  {@render stages(agent)}
                {/if}
              {:else if agent.status === 'queued' && agent.message}
                <span class="upd-sub">{agent.message}</span>
              {:else}
                <span class="upd-sub upd-dash">—</span>
              {/if}
            </td>
            <td class="upd-r">
              {#if isApplicable}
                <button class="btn sm" type="button" disabled={!canAct} onclick={() => run([agent.id])}>
                  {$trans('upd.agents.update')}
                </button>
              {/if}
            </td>
          </tr>
        {:else}
          <tr><td colspan="6" class="upd-sub">{$trans('upd.agents.empty')}</td></tr>
        {/each}
      </tbody>
    </table>
  </div>
</Fold>

<style>
  .more {
    margin: 4px 0 0;
    padding: 0;
    border: 0;
    background: none;
    color: var(--accent);
    font: inherit;
    font-size: 11px;
    text-decoration: underline;
    cursor: pointer;
  }

  .stages {
    margin-top: 5px;
  }
</style>
