<script lang="ts">
  import PixIcon from '../ui/PixIcon.svelte';
  import { onMount } from 'svelte';
  import type { AgentUpdateStep } from '../../api';
  import { locale, tr as trans } from '../../i18n';
  import { fmtBytes, fmtDuration } from './format';

  let {
    steps,
    nowMs = 0,
    rollbackAt = 0,
  }: {
    steps: AgentUpdateStep[];
    /** The server clock at the moment the data was made; the times of the stages are on that clock. */
    nowMs?: number;
    /** Unix seconds of the rollback of the agent; 0 when none is armed. */
    rollbackAt?: number;
  } = $props();

  /** How far the clock of this browser is ahead of the server's, so running timers do not depend on either. */
  let skew = $state(0);
  let tick = $state(Date.now());

  $effect(() => {
    skew = nowMs > 0 ? Date.now() - nowMs : 0;
  });

  onMount(() => {
    const timer = setInterval(() => (tick = Date.now()), 1000);
    return () => clearInterval(timer);
  });

  const serverNow = $derived(tick - skew);

  function elapsed(step: AgentUpdateStep): string {
    if (!step.started_ms) return '';
    const end = step.state === 'running' ? serverNow : step.finished_ms;
    if (!end) return '';
    return fmtDuration(end - step.started_ms, $trans, $locale);
  }

  /** The grey line under a stage: the size of the download, the version that came back, a countdown, an error. */
  function note(step: AgentUpdateStep): string {
    if (step.state === 'failed' && step.detail) return step.detail;
    if (step.id === 'download' && step.size && step.size > 0) {
      return step.state === 'running'
        ? $trans('upd.agstep.of', { done: fmtBytes(step.bytes ?? 0, $trans), total: fmtBytes(step.size, $trans) })
        : fmtBytes(step.size, $trans);
    }
    if (step.id === 'reconnect' && step.detail) return $trans('upd.agstep.version', { version: step.detail });
    if (rollbackAt > 0 && (step.id === 'offline' || step.id === 'reconnect' || step.id === 'confirm') && step.state === 'running') {
      const left = Math.ceil(rollbackAt - serverNow / 1000);
      return left > 0 ? $trans('upd.agstep.rollbackIn', { n: left }) : $trans('upd.agstep.rollbackDue');
    }
    return '';
  }
</script>

<ol class="steps" aria-label={$trans('upd.agstep.aria')}>
  {#each steps as step (step.id)}
    {@const text = note(step)}
    {@const time = elapsed(step)}
    <li class="step {step.state}">
      <span class="ico" aria-hidden="true">
        {#if step.state === 'done'}<PixIcon name="check" size={12} />{:else if step.state === 'running'}<span class="spin"></span>{:else if step.state === 'failed'}<PixIcon name="x" size={12} />{:else}<PixIcon name="circle" size={12} />{/if}
      </span>
      <span class="name">{$trans(`upd.agstep.${step.id}`)}</span>
      {#if time}<span class="dur">{time}</span>{/if}
      {#if text}<span class="note" class:bad={step.state === 'failed'}>{text}</span>{/if}
    </li>
  {/each}
</ol>

<style>
  .steps {
    margin: 0;
    padding: 0;
    list-style: none;
    display: flex;
    flex-direction: column;
    gap: 3px;
    font-size: 11px;
  }

  .step {
    display: grid;
    grid-template-columns: 13px 1fr auto;
    column-gap: 7px;
    align-items: baseline;
    color: var(--ink);
  }

  .pending {
    color: var(--muted);
  }

  .running {
    color: var(--accent);
  }

  .done .ico {
    color: var(--good);
  }

  .failed .ico,
  .failed .name {
    color: var(--crit);
  }

  .ico {
    display: inline-flex;
    justify-content: center;
  }

  .dur {
    color: var(--muted);
    white-space: nowrap;
  }

  .note {
    grid-column: 2 / -1;
    color: var(--muted);
    overflow-wrap: anywhere;
  }

  .note.bad {
    color: var(--crit);
  }

  .spin {
    display: inline-block;
    width: 9px;
    height: 9px;
    border: 2px solid currentColor;
    border-right-color: transparent;
    border-radius: 50%;
    animation: agent-step-spin 0.8s linear infinite;
  }

  @keyframes agent-step-spin {
    to {
      transform: rotate(360deg);
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .spin {
      animation: none;
    }
  }
</style>
