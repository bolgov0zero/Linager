<script lang="ts" module>
  export interface Stage {
    id: string;
    state: 'pending' | 'running' | 'done' | 'warn' | 'failed';
    /** Already translated name of the stage. */
    name: string;
    /** The grey line under the name; red on a failed stage, amber on a warning. */
    note?: string;
    /** Unix milliseconds on the server clock. */
    startedMs?: number;
    finishedMs?: number;
    /** 0..1: draws a progress bar under the note. */
    progress?: number;
  }
</script>

<script lang="ts">
  import { onMount } from 'svelte';
  import { locale, tr as trans } from '../../i18n';
  import { fmtDuration } from '../updates/format';

  /**
   * The list of stages of a long job (the same look as the agent update: ✓ ✗ ○, a spinner for the running one,
   * the time of every stage and a grey note). The times are on the server clock; `nowMs` is the server clock at the
   * moment the data was made, so the running timer does not depend on the clock of this browser.
   */
  let {
    stages,
    nowMs = 0,
    label,
  }: {
    stages: Stage[];
    nowMs?: number;
    label?: string;
  } = $props();

  let skew = $state(0);
  let tick = $state(Date.now());

  $effect(() => {
    skew = nowMs > 0 ? Date.now() - nowMs : 0;
  });

  const anyRunning = $derived(stages.some((s) => s.state === 'running'));

  onMount(() => {
    const timer = setInterval(() => {
      if (anyRunning) tick = Date.now();
    }, 250);
    return () => clearInterval(timer);
  });

  const serverNow = $derived(tick - skew);

  function elapsed(stage: Stage): string {
    if (!stage.startedMs) return '';
    const end = stage.state === 'running' ? serverNow : stage.finishedMs;
    if (!end) return '';
    return fmtDuration(end - stage.startedMs, $trans, $locale);
  }
</script>

<ol class="steps" aria-label={label}>
  {#each stages as stage (stage.id)}
    {@const time = elapsed(stage)}
    <li class="step {stage.state}">
      <span class="ico" aria-hidden="true">
        {#if stage.state === 'done'}✓{:else if stage.state === 'running'}<span class="spin"></span>{:else if stage.state === 'failed'}✗{:else if stage.state === 'warn'}!{:else}○{/if}
      </span>
      <span class="name">{stage.name}</span>
      {#if time}<span class="dur">{time}</span>{/if}
      {#if stage.note}<span class="note">{stage.note}</span>{/if}
      {#if stage.progress !== undefined}
        <span class="bar" role="progressbar" aria-valuemin="0" aria-valuemax="100" aria-valuenow={Math.round(stage.progress * 100)}>
          <i style="width: {Math.round(Math.min(Math.max(stage.progress, 0), 1) * 100)}%"></i>
        </span>
      {/if}
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
    gap: 5px;
    font-size: 12px;
  }

  .step {
    display: grid;
    grid-template-columns: 14px 1fr auto;
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

  .warn .ico,
  .warn .note {
    color: var(--warn);
  }

  .failed .ico,
  .failed .name,
  .failed .note {
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
    font-size: 11px;
    overflow-wrap: anywhere;
  }

  .bar {
    grid-column: 2 / -1;
    display: block;
    height: 7px;
    margin-top: 4px;
    border: 2px solid var(--line);
    background: var(--seg-off);
  }

  .bar i {
    display: block;
    height: 100%;
    background: var(--accent);
  }

  .spin {
    display: inline-block;
    width: 9px;
    height: 9px;
    border: 2px solid currentColor;
    border-right-color: transparent;
    border-radius: 50%;
    animation: stage-spin 0.8s linear infinite;
  }

  @keyframes stage-spin {
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
