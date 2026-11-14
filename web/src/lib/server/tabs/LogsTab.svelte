<script lang="ts">
  import { openStream, type ServerDetail } from '../../../api';
  import Select from '../../ui/Select.svelte';
  import { tr } from '../../../i18n';
  import { LOG_LIMIT, LogLines, logTone, type LogTone } from './logs-lines';

  let { server }: { server: ServerDetail } = $props();

  interface Row {
    id: number;
    text: string;
    tone: LogTone;
  }

  const PRESETS = ['ssh', 'cron', 'nginx', 'docker'];

  // Source: all | kernel | a preset unit | custom (typed unit).
  let source = $state('all');
  let unitDraft = $state('');
  let unitApplied = $state('');
  let priority = $state('7');
  let paused = $state(false);
  let autoscroll = $state(true);
  let rows = $state<Row[]>([]);
  let streamState = $state<'connecting' | 'open' | 'closed'>('connecting');
  let streamReason = $state<string | null>(null);
  let attempt = $state(0);
  let box = $state<HTMLDivElement | null>(null);

  const kernel = $derived(source === 'kernel');
  const unit = $derived(kernel ? '' : unitApplied);

  // Lines are buffered without reactivity; the visible list is copied at most once per frame,
  // and not at all while paused, so pausing only stops rendering.
  let buffer: Row[] = [];
  let nextId = 0;
  let flushQueued = false;
  let current: { close: () => void } | null = null;

  function pushLine(text: string): void {
    buffer.push({ id: ++nextId, text, tone: logTone(text) });
    if (buffer.length > LOG_LIMIT) buffer.splice(0, buffer.length - LOG_LIMIT);
  }

  function flush(): void {
    if (paused || flushQueued) return;
    flushQueued = true;
    requestAnimationFrame(() => {
      flushQueued = false;
      if (!paused) rows = buffer.slice();
    });
  }

  function clearLines(): void {
    buffer = [];
    rows = [];
  }

  function togglePause(): void {
    paused = !paused;
    if (!paused) flush();
  }

  function pickSource(value: string): void {
    source = value;
    if (value === 'all' || value === 'kernel') {
      unitDraft = '';
      unitApplied = '';
    } else if (value !== 'custom') {
      unitDraft = value;
      unitApplied = value;
    }
  }

  // Applied on change (blur or Enter), not per keystroke, so typing does not reconnect.
  function commitUnit(): void {
    unitApplied = unitDraft.trim();
    source = unitApplied === '' ? 'all' : PRESETS.includes(unitApplied) ? unitApplied : 'custom';
  }

  // Each change of unit, kernel or priority opens a new stream; the old one is closed.
  $effect(() => {
    const online = server.online;
    const id = server.id;
    const u = unit;
    const k = kernel;
    const p = Number(priority);
    void attempt;
    if (!online) return;

    clearLines();
    const assembler = new LogLines();
    const socket = openStream(id, 'logs', { unit: u, priority: p, kernel: k ? 1 : 0, lines: 200 });
    let active = true;
    streamState = 'connecting';
    streamReason = null;

    socket.onopen = () => {
      if (active) streamState = 'open';
    };

    socket.onmessage = (event: MessageEvent<unknown>) => {
      if (!active) return;
      if (typeof event.data === 'string') {
        try {
          const msg = JSON.parse(event.data) as { error?: unknown; closed?: unknown };
          if (typeof msg.error === 'string') streamReason = msg.error;
          else if (typeof msg.closed === 'string') streamReason = msg.closed;
        } catch {
          // not JSON - ignore
        }
        return;
      }
      for (const line of assembler.push(event.data as ArrayBuffer)) pushLine(line);
      flush();
    };

    socket.onclose = () => {
      if (!active) return;
      for (const line of assembler.flush()) pushLine(line);
      streamState = 'closed';
      flush();
    };

    current = { close: () => socket.close() };

    return () => {
      active = false;
      socket.close();
      current = null;
    };
  });

  // Keep the newest line in view when autoscroll is on.
  $effect(() => {
    void rows;
    if (autoscroll && box) box.scrollTop = box.scrollHeight;
  });
</script>

<div class="logs">
  {#if !server.online}
    <p class="note">{$tr('logs.offline')}</p>
  {:else}
    <div class="controls">
      <div class="group">
        <label class="ctl">
          <span class="label">{$tr('logs.source')}</span>
          <Select
            value={source}
            onchange={pickSource}
            options={[
              { value: 'all', label: $tr('logs.source.all') },
              { value: 'kernel', label: $tr('logs.source.kernel') },
              { value: 'ssh', label: 'ssh' },
              { value: 'cron', label: 'cron' },
              { value: 'nginx', label: 'nginx' },
              { value: 'docker', label: 'docker' },
              ...(source === 'custom' ? [{ value: 'custom', label: $tr('logs.source.custom') }] : []),
            ]}
          />
        </label>
        <label class="ctl">
          <span class="label">{$tr('logs.unit')}</span>
          <input
            class="input"
            type="text"
            placeholder={$tr('logs.unitPlaceholder')}
            autocomplete="off"
            spellcheck="false"
            disabled={kernel}
            bind:value={unitDraft}
            onchange={commitUnit}
            onkeydown={(e) => {
              if (e.key === 'Enter') commitUnit();
            }}
          />
        </label>
        <label class="ctl">
          <span class="label">{$tr('logs.priority')}</span>
          <Select
            bind:value={priority}
            options={[
              { value: '7', label: $tr('logs.priority.all') },
              { value: '3', label: $tr('logs.priority.err') },
              { value: '4', label: $tr('logs.priority.warn') },
              { value: '6', label: $tr('logs.priority.info') },
            ]}
          />
        </label>
      </div>

      <div class="row">
        <button class="btn" type="button" aria-pressed={paused} onclick={togglePause}>
          {paused ? $tr('logs.resume') : $tr('logs.pause')}
        </button>
        <button class="btn" type="button" onclick={clearLines}>{$tr('logs.clear')}</button>
        <button class="chip" type="button" aria-pressed={autoscroll} onclick={() => (autoscroll = !autoscroll)}>
          {$tr('logs.autoscroll')}
        </button>
      </div>
    </div>

    {#if paused}
      <p class="hint">{$tr('logs.pausedHint')}</p>
    {/if}

    <div class="log" bind:this={box} role="log" aria-live="off">
      {#each rows as row (row.id)}
        <div class="line {row.tone}">{row.text}</div>
      {:else}
        <div class="line muted-line">{$tr('logs.empty')}</div>
      {/each}
    </div>

    {#if streamState === 'closed'}
      <div class="closed">
        <p class="error" role="alert">{streamReason ?? $tr('logs.closed')}</p>
        <button class="btn" type="button" onclick={() => attempt++}>{$tr('logs.reconnect')}</button>
      </div>
    {/if}
  {/if}
</div>

<style>
  .logs {
    display: flex;
    flex-direction: column;
    gap: 12px;
    min-width: 0;
  }

  .controls {
    display: flex;
    flex-wrap: wrap;
    align-items: flex-end;
    justify-content: space-between;
    gap: 12px;
  }

  .group {
    display: flex;
    flex-wrap: wrap;
    gap: 12px;
  }

  .ctl {
    display: flex;
    flex-direction: column;
    gap: 4px;
    min-width: 160px;
  }

  .ctl .label {
    color: var(--muted);
    font-size: 12px;
  }

  .ctl .input {
    width: auto;
    min-width: 160px;
  }

  .hint {
    margin: 0;
    color: var(--warn);
    font-size: 12px;
  }

  .log {
    height: 420px;
    overflow: auto;
    padding: 10px 12px;
    background: #0f1218;
    color: #e6e9ef;
    border: 2px solid var(--line);
    font-size: 12px;
    line-height: 1.5;
  }

  .line {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }

  .line.err {
    color: #ff6d60;
  }

  .line.warn {
    color: #e6b04b;
  }

  .muted-line {
    color: #8a93a4;
  }

  .note {
    margin: 0;
    padding: 24px 0;
    text-align: center;
    color: var(--muted);
  }

  .closed {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 12px;
  }

  @media (max-width: 600px) {
    .log {
      height: 320px;
    }

    .ctl,
    .ctl .input {
      min-width: 0;
      width: 100%;
    }
  }
</style>
