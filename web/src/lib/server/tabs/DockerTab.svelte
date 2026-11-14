<script lang="ts">
  import { get } from 'svelte/store';
  import { untrack } from 'svelte';
  import { api, openStream, type DockerContainer, type DockerImage, type DockerInfo, type ServerDetail } from '../../../api';
  import { locale, t, tr as tStore, type Vars } from '../../../i18n';
  import Modal from '../../Modal.svelte';
  import { showToast } from '../../toast';
  import { formatBytes, formatDateTime } from '../format';
  import {
    attachStream,
    createBatcher,
    cpuText,
    errorText,
    imageTitle,
    isRunning,
    LINE_LIMIT,
    ramText,
    shortId,
    stateTone,
    unavailable,
    type StreamLine,
    type StreamState,
  } from './docker-util';

  let { server }: { server: ServerDetail } = $props();

  type ContainerAction = 'start' | 'stop' | 'restart' | 'remove';
  type Section = 'containers' | 'images';
  type LoadState = 'loading' | 'ready' | 'offline' | 'disabled' | 'error';

  interface StreamView {
    kind: 'logs' | 'pull';
    title: string;
    lines: StreamLine[];
    state: StreamState;
  }

  interface Ask {
    title: string;
    text: string;
    label: string;
    run: () => Promise<void>;
  }

  const POLL_MS = 5000;

  // Only the id drives polling. Other fields change in place on live events and must not restart it.
  const serverId = $derived(server.id);

  let section = $state<Section>('containers');
  let info = $state<DockerInfo | null>(null);
  let loadState = $state<LoadState>('loading');
  let loadError = $state('');
  let seq = 0;

  let busy = $state<string | null>(null);
  let ask = $state<Ask | null>(null);
  let askBusy = $state(false);

  let stream = $state<StreamView | null>(null);
  let autoscroll = $state(true);
  let logBox = $state<HTMLDivElement | undefined>(undefined);
  let pullInput = $state('');
  let pullError = $state('');

  // The open socket of the logs or pull modal, if any.
  let detach: (() => void) | null = null;
  let batcher: ReturnType<typeof createBatcher> | null = null;

  const containers = $derived<DockerContainer[]>(info?.containers ?? []);
  const images = $derived<DockerImage[]>(info?.images ?? []);
  const runningCount = $derived(containers.filter(isRunning).length);
  const offline = $derived(loadState === 'offline' || !server.online);
  const readOnly = $derived(offline || loadState === 'disabled');

  function T(key: string, vars?: Vars): string {
    return t(key, get(locale), vars);
  }

  /** Keeps the previous data on failure; the list is only replaced by a fresh answer. */
  async function refresh(id: string = serverId): Promise<void> {
    const req = ++seq;
    try {
      const data = await api.docker(id);
      if (req !== seq) return;
      info = data;
      loadState = 'ready';
      loadError = '';
    } catch (e) {
      if (req !== seq) return;
      const kind = unavailable(e);
      if (kind) {
        loadState = kind;
        return;
      }
      loadState = 'error';
      loadError = errorText(e, T);
    }
  }

  $effect(() => {
    const id = serverId;
    untrack(() => {
      info = null;
      loadState = 'loading';
      void refresh(id);
    });
    const timer = setInterval(() => void refresh(id), POLL_MS);
    return () => clearInterval(timer);
  });

  // Close any open socket when the tab unmounts.
  $effect(() => {
    return () => stopStream();
  });

  function stopStream(): void {
    detach?.();
    detach = null;
    batcher?.cancel();
    batcher = null;
  }

  function closeStream(): void {
    const kind = stream?.kind;
    stopStream();
    stream = null;
    if (kind === 'pull') void refresh();
  }

  /** Attaches a live stream to a view; the view object must be the reactive proxy from `stream`. */
  function connect(view: StreamView, ws: WebSocket): void {
    const batch = createBatcher((lines) => {
      view.lines.push(...lines);
      if (view.lines.length > LINE_LIMIT) view.lines.splice(0, view.lines.length - LINE_LIMIT);
    });
    batcher = batch;
    detach = attachStream(ws, {
      open: () => {
        view.state = 'live';
      },
      lines: (lines) => batch.push(lines),
      end: (failed) => {
        batch.flushNow();
        view.state = failed ? 'failed' : 'ended';
        if (view.kind === 'pull') void refresh();
      },
    });
  }

  function openLogs(c: DockerContainer): void {
    stopStream();
    stream = { kind: 'logs', title: T('docker.logsTitle', { name: c.name }), lines: [], state: 'connecting' };
    connect(stream, openStream(serverId, 'docker-logs', { container: c.id, lines: 200 }));
  }

  function startPull(): void {
    const image = pullInput.trim();
    if (!image) {
      pullError = T('docker.pull.emptyError');
      return;
    }
    pullError = '';
    stopStream();
    stream = { kind: 'pull', title: T('docker.pull.title', { image }), lines: [], state: 'connecting' };
    connect(stream, openStream(serverId, 'docker-pull', { image }));
  }

  $effect(() => {
    void stream?.lines.length;
    if (autoscroll && logBox) logBox.scrollTop = logBox.scrollHeight;
  });

  async function containerAction(c: DockerContainer, action: ContainerAction): Promise<void> {
    const id = serverId;
    busy = `${c.id}:${action}`;
    try {
      await api.dockerContainerAction(id, c.id, action);
      showToast(T('docker.done', { action: T(`docker.act.${action}`), name: c.name }));
      await refresh(id);
    } catch (e) {
      showToast(errorText(e, T));
    } finally {
      busy = null;
    }
  }

  function rowBusy(c: DockerContainer): boolean {
    return busy !== null && busy.startsWith(`${c.id}:`);
  }

  function requestRemove(c: DockerContainer): void {
    ask = {
      title: T('docker.removeTitle'),
      text: T('docker.removeText', { name: c.name }),
      label: T('docker.act.remove'),
      run: () => containerAction(c, 'remove'),
    };
  }

  function requestImageRemove(image: DockerImage): void {
    const id = serverId;
    ask = {
      title: T('docker.removeImageTitle'),
      text: T('docker.removeImageText', { name: imageTitle(image) }),
      label: T('docker.act.remove'),
      run: async () => {
        try {
          await api.dockerRemoveImage(id, image.id);
          showToast(T('docker.imageDeleted'));
          await refresh(id);
        } catch (e) {
          showToast(errorText(e, T));
        }
      },
    };
  }

  async function confirmAsk(): Promise<void> {
    const current = ask;
    if (!current || askBusy) return;
    askBusy = true;
    try {
      await current.run();
    } finally {
      askBusy = false;
      ask = null;
    }
  }
</script>

<div class="docker">
  <div class="toolbar">
    <div class="row" role="group" aria-label={$tStore('docker.sectionsLabel')}>
      <button class="chip" type="button" aria-pressed={section === 'containers'} onclick={() => (section = 'containers')}>
        {$tStore('docker.tabs.containers')} <span class="count">{containers.length}</span>
      </button>
      <button class="chip" type="button" aria-pressed={section === 'images'} onclick={() => (section = 'images')}>
        {$tStore('docker.tabs.images')} <span class="count">{images.length}</span>
      </button>
    </div>
    {#if info?.available}
      <span class="small muted">{$tStore('docker.summary', { running: runningCount, total: containers.length })}</span>
    {/if}
  </div>

  {#if offline}
    <p class="notice" role="status">{$tStore('docker.offline')}</p>
  {:else if loadState === 'disabled'}
    <p class="notice" role="status">{$tStore('docker.disabled')}</p>
  {:else if loadState === 'error' && info}
    <p class="notice" role="status">{$tStore('docker.refreshFailed')}: {loadError}</p>
  {/if}

  {#if !info}
    {#if loadState === 'error'}
      <div class="state">
        <p class="error" role="alert">{$tStore('docker.loadFailed')}: {loadError}</p>
        <div class="row">
          <button class="btn" type="button" onclick={() => refresh()}>{$tStore('docker.retry')}</button>
        </div>
      </div>
    {:else if loadState === 'loading'}
      <p class="muted">{$tStore('common.loading')}</p>
    {/if}
  {:else if !info.available}
    <p class="muted">{$tStore('docker.unavailable')}</p>
  {:else if section === 'containers'}
    {#if containers.length === 0}
      <p class="muted">{$tStore('docker.noContainers')}</p>
    {:else}
      <div class="tablewrap">
        <table class="grid">
          <thead>
            <tr>
              <th>{$tStore('docker.col.container')}</th>
              <th>{$tStore('docker.col.state')}</th>
              <th class="hide-sm">{$tStore('docker.col.ports')}</th>
              <th class="num">{$tStore('docker.col.cpu')}</th>
              <th class="num hide-sm">{$tStore('docker.col.ram')}</th>
              <th>{$tStore('docker.col.status')}</th>
              <th class="acts-head"><span class="sr-only">{$tStore('docker.col.actions')}</span></th>
            </tr>
          </thead>
          <tbody>
            {#each containers as c (c.id)}
              {@const running = isRunning(c)}
              <tr>
                <td class="name">
                  <div>{c.name}</div>
                  <div class="muted small mono">{c.image}</div>
                </td>
                <td>
                  <span class="badge {stateTone(c.state)}">{c.state}</span>
                </td>
                <td class="hide-sm mono small">{c.ports.length ? c.ports.join(', ') : '—'}</td>
                <td class="num mono small">{cpuText(c)}</td>
                <td class="num mono small hide-sm">{ramText(c)}</td>
                <td class="small">{c.status}</td>
                <td class="acts">
                  <div class="row end">
                    <button class="btn" type="button" disabled={readOnly} onclick={() => openLogs(c)}>{$tStore('docker.logs')}</button>
                    <button class="btn" type="button" disabled={readOnly || running || rowBusy(c)} onclick={() => containerAction(c, 'start')}>
                      {$tStore('docker.act.start')}
                    </button>
                    <button class="btn" type="button" disabled={readOnly || !running || rowBusy(c)} onclick={() => containerAction(c, 'restart')}>
                      {$tStore('docker.act.restart')}
                    </button>
                    <button class="btn" type="button" disabled={readOnly || !running || rowBusy(c)} onclick={() => containerAction(c, 'stop')}>
                      {$tStore('docker.act.stop')}
                    </button>
                    <button class="btn" type="button" disabled={readOnly || rowBusy(c)} onclick={() => requestRemove(c)}>
                      {$tStore('docker.act.remove')}
                    </button>
                  </div>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {/if}
  {:else}
    {#if images.length === 0}
      <p class="muted">{$tStore('docker.noImages')}</p>
    {:else}
      <div class="tablewrap">
        <table class="grid">
          <thead>
            <tr>
              <th>{$tStore('docker.col.image')}</th>
              <th class="hide-sm">{$tStore('docker.col.id')}</th>
              <th class="num">{$tStore('docker.col.size')}</th>
              <th class="hide-sm">{$tStore('docker.col.created')}</th>
              <th>{$tStore('docker.col.usage')}</th>
              <th class="acts-head"><span class="sr-only">{$tStore('docker.col.actions')}</span></th>
            </tr>
          </thead>
          <tbody>
            {#each images as img (img.id)}
              {@const tags = img.repo_tags ?? []}
              <tr>
                <td class="name">
                  <div class="mono">{imageTitle(img)}</div>
                  {#if tags.length > 1}
                    <div class="muted small mono">{tags.slice(1).join(', ')}</div>
                  {/if}
                </td>
                <td class="hide-sm mono small">{shortId(img.id)}</td>
                <td class="num mono small">{formatBytes(img.size)}</td>
                <td class="hide-sm small">{img.created ? formatDateTime(img.created, $locale) : '—'}</td>
                <td>
                  {#if img.used}
                    <span class="badge accent">{$tStore('docker.used')}</span>
                  {/if}
                </td>
                <td class="acts">
                  <div class="row end">
                    <button
                      class="btn"
                      type="button"
                      disabled={readOnly || img.used}
                      title={img.used ? $tStore('docker.usedHint') : undefined}
                      onclick={() => requestImageRemove(img)}
                    >
                      {$tStore('docker.act.remove')}
                    </button>
                  </div>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {/if}

    <form
      class="pull"
      onsubmit={(e) => {
        e.preventDefault();
        startPull();
      }}
    >
      <label class="field pull-field">
        <span class="label">{$tStore('docker.pull.label')}</span>
        <input
          class="input mono"
          type="text"
          placeholder="nginx:1.25-alpine"
          autocomplete="off"
          spellcheck="false"
          bind:value={pullInput}
          disabled={readOnly}
        />
      </label>
      <button class="btn primary" type="submit" disabled={readOnly}>{$tStore('docker.pull.submit')}</button>
      {#if pullError}
        <p class="error" role="alert">{pullError}</p>
      {/if}
    </form>
  {/if}
</div>

{#if stream}
  {@const s = stream}
  <div class="wide">
    <Modal title={s.title} labelId="docker-stream-title" onclose={closeStream}>
      <div class="row between">
        <span class="small muted">{$tStore(`docker.stream.${s.state}`)}</span>
        {#if s.kind === 'logs'}
          <label class="check">
            <input type="checkbox" bind:checked={autoscroll} />
            <span>{$tStore('docker.autoscroll')}</span>
          </label>
        {/if}
      </div>
      <div class="logpanel mono" bind:this={logBox} role="log">
        {#each s.lines as line, i (i)}
          <div class="ln {line.tone}">{line.text}</div>
        {:else}
          <div class="ln info">{$tStore(s.kind === 'logs' ? 'docker.logsEmpty' : 'docker.pull.waiting')}</div>
        {/each}
      </div>
    </Modal>
  </div>
{/if}

{#if ask}
  {@const a = ask}
  <Modal
    title={a.title}
    labelId="docker-ask-title"
    onclose={() => {
      if (!askBusy) ask = null;
    }}
  >
    <p>{a.text}</p>
    <div class="row end">
      <button class="btn" type="button" disabled={askBusy} onclick={() => (ask = null)}>{$tStore('common.cancel')}</button>
      <button class="btn danger" type="button" disabled={askBusy} onclick={confirmAsk}>{a.label}</button>
    </div>
  </Modal>
{/if}

<style>
  .docker {
    display: flex;
    flex-direction: column;
    gap: 12px;
    min-width: 0;
  }

  .toolbar {
    display: flex;
    flex-wrap: wrap;
    justify-content: space-between;
    align-items: center;
    gap: 8px;
  }

  .toolbar .count {
    margin-left: 4px;
  }

  .notice {
    margin: 0;
    padding: 8px 12px;
    border: 2px solid var(--warn);
    color: var(--warn);
  }

  .state {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 12px;
  }

  .tablewrap {
    overflow-x: auto;
  }

  table.grid {
    width: 100%;
    min-width: 520px;
    border-collapse: collapse;
  }

  th {
    padding: 8px;
    border-bottom: 2px solid var(--line);
    color: var(--muted);
    font-size: 12px;
    font-weight: 400;
    letter-spacing: 0.04em;
    text-align: left;
    text-transform: uppercase;
    white-space: nowrap;
  }

  td {
    padding: 8px;
    border-bottom: 2px dashed var(--seg-off);
    vertical-align: middle;
  }

  .num {
    text-align: right;
    white-space: nowrap;
  }

  .name {
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .acts {
    width: 1%;
    white-space: nowrap;
  }

  .acts-head {
    width: 1%;
  }

  .badge {
    display: inline-block;
    padding: 1px 6px;
    border: 2px solid var(--muted);
    color: var(--muted);
    font-size: 12px;
    white-space: nowrap;
  }

  .badge.good {
    border-color: var(--good);
    color: var(--good);
  }

  .badge.warn {
    border-color: var(--warn);
    color: var(--warn);
  }

  .badge.crit {
    border-color: var(--crit);
    color: var(--crit);
  }

  .badge.accent {
    border-color: var(--accent);
    color: var(--accent);
  }

  .pull {
    display: flex;
    flex-wrap: wrap;
    align-items: flex-end;
    gap: 8px;
  }

  .pull-field {
    flex: 1 1 240px;
    min-width: 0;
  }

  .pull > .error {
    flex-basis: 100%;
  }

  .between {
    justify-content: space-between;
  }

  .wide :global(.dialog) {
    max-width: 900px;
  }

  .logpanel {
    height: min(60vh, 520px);
    overflow: auto;
    padding: 10px;
    border: 2px solid var(--line);
    background: #0c0f14;
    color: #d8dee9;
    font-size: 12px;
    line-height: 1.4;
  }

  .ln {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }

  .ln.info {
    color: #8a93a4;
  }

  .ln.error {
    color: #ff8a80;
  }

  .sr-only {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip: rect(0 0 0 0);
    white-space: nowrap;
  }

  @media (max-width: 720px) {
    .hide-sm {
      display: none;
    }

    table.grid {
      min-width: 0;
    }
  }
</style>
