<script lang="ts">
  import { get } from 'svelte/store';
  import { untrack } from 'svelte';
  import { api, openStream, type DockerContainer, type DockerImage, type DockerInfo, type ServerDetail } from '../../../api';
  import { locale, t, tr as tStore, type Vars } from '../../../i18n';
  import Modal from '../../Modal.svelte';
  import ActionMenu from '../../ui/ActionMenu.svelte';
  import { showToast } from '../../toast';
  import { formatBytes, formatDateTime } from '../format';
  import {
    attachStream,
    createBatcher,
    cpuText,
    errorText,
    groupContainers,
    imageTitle,
    isRunning,
    LINE_LIMIT,
    ramText,
    shortId,
    stateTone,
    unavailable,
    type ContainerGroup,
    type StreamLine,
    type StreamState,
  } from './docker-util';

  let { server }: { server: ServerDetail } = $props();

  type ContainerAction = 'start' | 'stop' | 'restart' | 'remove';
  type Section = 'containers' | 'images';
  type LoadState = 'loading' | 'ready' | 'offline' | 'disabled' | 'error';

  interface StreamView {
    kind: 'logs' | 'pull' | 'update';
    title: string;
    lines: StreamLine[];
    state: StreamState;
  }

  interface Ask {
    title: string;
    text: string;
    label: string;
    /** A destructive action gets a red button, an update a normal accent one. */
    danger: boolean;
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
  /** The "update the container" dialog: image is only used for a plain container (compose has it in its file). */
  let updateFor = $state<{ c: DockerContainer; image: string } | null>(null);
  /** Open groups by key; compose projects start collapsed, the containers without a project open. */
  let openGroups = $state<Record<string, boolean>>({});
  let checking = $state(false);
  let pullInput = $state('');
  let pullError = $state('');

  // The open socket of the logs or pull modal, if any.
  let detach: (() => void) | null = null;
  let batcher: ReturnType<typeof createBatcher> | null = null;

  const containers = $derived<DockerContainer[]>(info?.containers ?? []);
  const images = $derived<DockerImage[]>(info?.images ?? []);
  const runningCount = $derived(containers.filter(isRunning).length);
  const groups = $derived(groupContainers(containers));
  const hasProjects = $derived(groups.some((g) => g.key !== ''));
  const updatable = $derived(containers.filter((c) => c.upd === 'new'));
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
      openGroups = {};
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
    if (kind === 'pull' || kind === 'update') void refresh();
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
        if (view.kind !== 'logs') void refresh();
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

  function requestUpdate(c: DockerContainer): void {
    updateFor = { c, image: c.image };
  }

  /** Pulls the image again and recreates the container (through compose for a compose project). */
  function startUpdate(): void {
    const target = updateFor;
    if (!target) return;
    updateFor = null;
    const { c, image } = target;
    const params: Record<string, string> = { container: c.id };
    if (!c.compose && image.trim() && image.trim() !== c.image) params.image = image.trim();
    stopStream();
    stream = { kind: 'update', title: T('docker.update.streamTitle', { name: c.name }), lines: [], state: 'connecting' };
    connect(stream, openStream(serverId, 'docker-update', params));
  }

  function isOpen(g: ContainerGroup): boolean {
    return openGroups[g.key] ?? g.key === '';
  }

  function toggleGroup(g: ContainerGroup): void {
    openGroups[g.key] = !isOpen(g);
  }

  async function checkImages(): Promise<void> {
    if (checking || readOnly) return;
    const id = serverId;
    checking = true;
    try {
      const res = await api.dockerCheck(id);
      showToast(T('docker.check.done', { n: res.new }));
      await refresh(id);
    } catch (e) {
      showToast(errorText(e, T));
    } finally {
      checking = false;
    }
  }

  function requestUpdateMarked(): void {
    const list = updatable;
    if (list.length === 0) return;
    ask = {
      title: T('docker.marked.title'),
      text: T('docker.marked.text', { names: list.map((c) => c.name).join(', ') }),
      label: T('docker.update.go'),
      danger: false,
      run: async () => {
        stopStream();
        stream = { kind: 'update', title: T('docker.marked.title'), lines: [], state: 'connecting' };
        connect(stream, openStream(serverId, 'docker-update', { containers: list.map((c) => c.id).join(',') }));
      },
    };
  }

  function requestUpdateProject(g: ContainerGroup): void {
    ask = {
      title: T('docker.grp.updateTitle', { name: g.key }),
      text: T('docker.grp.updateText'),
      label: T('docker.update.go'),
      danger: false,
      run: async () => {
        stopStream();
        stream = { kind: 'update', title: T('docker.grp.updateTitle', { name: g.key }), lines: [], state: 'connecting' };
        connect(stream, openStream(serverId, 'docker-update', { container: g.items[0].id, project: 1 }));
      },
    };
  }

  function openProjectLogs(g: ContainerGroup): void {
    stopStream();
    stream = { kind: 'logs', title: T('docker.grp.logsTitle', { name: g.key }), lines: [], state: 'connecting' };
    connect(stream, openStream(serverId, 'docker-project-logs', { container: g.items[0].id, lines: 200 }));
  }

  /** Start or restart every container of a project, one after the other. */
  async function projectAction(g: ContainerGroup, action: 'start' | 'restart'): Promise<void> {
    const id = serverId;
    busy = `group:${g.key}`;
    try {
      for (const c of g.items) {
        if (action === 'restart' && !isRunning(c)) continue;
        if (action === 'start' && isRunning(c)) continue;
        await api.dockerContainerAction(id, c.id, action);
      }
      showToast(T('docker.grp.done', { name: g.key, action: T(`docker.act.${action}`) }));
      await refresh(id);
    } catch (e) {
      showToast(errorText(e, T));
    } finally {
      busy = null;
    }
  }

  function checkedText(at: number | undefined): string {
    if (!at) return T('docker.check.never');
    const time = new Date(at * 1000).toLocaleTimeString($locale, { hour: '2-digit', minute: '2-digit' });
    return T('docker.check.at', { time });
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

  /** Items of the actions menu of a container row. */
  function containerMenu(c: DockerContainer, running: boolean) {
    const busy = readOnly || rowBusy(c);
    return [
      { key: 'logs', label: $tStore('docker.logs'), disabled: readOnly, onselect: () => openLogs(c) },
      running
        ? { key: 'stop', label: $tStore('docker.act.stop'), disabled: busy, onselect: () => void containerAction(c, 'stop') }
        : { key: 'start', label: $tStore('docker.act.start'), disabled: busy, onselect: () => void containerAction(c, 'start') },
      { key: 'restart', label: $tStore('docker.act.restart'), disabled: busy || !running, onselect: () => void containerAction(c, 'restart') },
      { key: 'update', label: $tStore('docker.act.update'), disabled: busy, onselect: () => requestUpdate(c) },
      { key: 'remove', label: $tStore('docker.act.remove'), disabled: busy, onselect: () => requestRemove(c) },
    ];
  }

  /** An image in use cannot be removed (the item is disabled). */
  function imageMenu(image: DockerImage) {
    return [{ key: 'remove', label: $tStore('docker.act.remove'), disabled: readOnly || image.used, onselect: () => requestImageRemove(image) }];
  }

  function requestRemove(c: DockerContainer): void {
    ask = {
      title: T('docker.removeTitle'),
      text: T('docker.removeText', { name: c.name }),
      label: T('docker.act.remove'),
      danger: true,
      run: () => containerAction(c, 'remove'),
    };
  }

  function requestImageRemove(image: DockerImage): void {
    const id = serverId;
    ask = {
      title: T('docker.removeImageTitle'),
      text: T('docker.removeImageText', { name: imageTitle(image) }),
      label: T('docker.act.remove'),
      danger: true,
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
      <div class="row checkbar">
        {#if section === 'containers'}
          <span class="small muted" title={$tStore('docker.check.help')}>
            {checkedText(info.checked_at)}{#if info.checked_at && updatable.length > 0}
              · {$tStore('docker.check.avail', { n: updatable.length })}{/if}
          </span>
          <button class="btn" type="button" disabled={readOnly || checking} onclick={() => void checkImages()}>
            {checking ? $tStore('docker.check.busy') : $tStore('docker.check.btn')}
          </button>
          <button class="btn primary" type="button" disabled={readOnly || updatable.length === 0} onclick={requestUpdateMarked}>
            {$tStore('docker.updateMarked')}
          </button>
        {:else}
          <span class="small muted">{$tStore('docker.summary', { running: runningCount, total: containers.length })}</span>
        {/if}
      </div>
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
      {#if hasProjects}
        {#each groups as g (g.key)}
          {@const open = isOpen(g)}
          <section class="grp" class:closed={!open}>
            <div class="grp-h">
              <button class="grp-toggle" type="button" aria-expanded={open} aria-label={$tStore('docker.grp.toggle')} onclick={() => toggleGroup(g)}>
                <span class="caret" aria-hidden="true"></span>
                <h4>{g.key || $tStore('docker.grp.plain')}</h4>
                {#if g.key}<span class="badge">compose</span>{/if}
              </button>
              <span class="small muted grp-sum">
                {$tStore('docker.grp.count', { n: g.items.length })} ·
                {g.running === g.items.length ? $tStore('docker.grp.allRunning') : $tStore('docker.grp.running', { running: g.running, total: g.items.length })}
                {#if g.file}· <span class="mono">{g.file}</span>{/if}
              </span>
              <div class="grp-acts">
                {#if g.updatable > 0}<span class="badge warn">{$tStore('docker.grp.updatable', { n: g.updatable })}</span>{/if}
                {#if g.key}
                  {@const gb = readOnly || busy === `group:${g.key}`}
                  <button class="btn sm" type="button" disabled={readOnly} onclick={() => requestUpdateProject(g)}>{$tStore('docker.grp.update')}</button>
                  {#if g.running > 0}
                    <button class="btn sm" type="button" disabled={gb} onclick={() => void projectAction(g, 'restart')}>{$tStore('docker.grp.restart')}</button>
                  {/if}
                  {#if g.running < g.items.length}
                    <button class="btn sm" type="button" disabled={gb} onclick={() => void projectAction(g, 'start')}>{$tStore('docker.grp.start')}</button>
                  {/if}
                  <button class="btn sm" type="button" disabled={readOnly} onclick={() => openProjectLogs(g)}>{$tStore('docker.grp.logs')}</button>
                {/if}
              </div>
            </div>
            {#if open}
              {@render containerTable(g.items)}
            {/if}
          </section>
        {/each}
      {:else}
        {@render containerTable(containers)}
      {/if}
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
                  <ActionMenu label={$tStore('docker.menu')} items={imageMenu(img)} disabled={readOnly} />
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

{#snippet containerTable(list: DockerContainer[])}
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
          <th>{$tStore('docker.col.registry')}</th>
          <th class="acts-head"><span class="sr-only">{$tStore('docker.col.actions')}</span></th>
        </tr>
      </thead>
      <tbody>
        {#each list as c (c.id)}
          {@const running = isRunning(c)}
          <tr class:hl={c.upd === 'new'}>
            <td class="name">
              <div>{c.name}</div>
              <div class="muted small mono">{c.image}</div>
            </td>
            <td>
              <span class="badge {stateTone(c.state)}">{c.state}</span>
            </td>
            <td class="hide-sm mono small">{c.ports.length ? c.ports.join(', ') : '—'}</td>
            <td class="num mono small" data-label="CPU">{cpuText(c)}</td>
            <td class="num mono small hide-sm">{ramText(c)}</td>
            <td class="small">{c.status}</td>
            <td class="reg small">
              {#if c.upd === 'new'}
                <span class="badge warn">{$tStore('docker.upd.new')}</span>
                {#if c.upd_local && c.upd_remote}<span class="muted mono">{c.upd_local} → {c.upd_remote}</span>{/if}
              {:else if c.upd === 'current'}
                <span class="badge good">{$tStore('docker.upd.current')}</span>
              {:else if c.upd === 'unknown'}
                <span class="badge" title={c.upd_error || $tStore('docker.upd.unknownHint')}>{$tStore('docker.upd.unknown')}</span>
              {:else}
                <span class="muted">{$tStore('docker.upd.none')}</span>
              {/if}
            </td>
            <td class="acts">
              <ActionMenu label={$tStore('docker.menu')} items={containerMenu(c, running)} disabled={readOnly} />
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
{/snippet}

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

{#if updateFor}
  {@const u = updateFor}
  <Modal title={T('docker.update.title', { name: u.c.name })} labelId="docker-update-title" onclose={() => (updateFor = null)}>
    <p>{$tStore('docker.update.text')}</p>
    {#if u.c.compose}
      <p class="muted">{$tStore('docker.update.compose')}</p>
    {:else}
      <label class="field">
        <span class="label">{$tStore('docker.update.image')}</span>
        <input class="input mono" type="text" spellcheck="false" bind:value={updateFor.image} />
      </label>
    {/if}
    <div class="row end">
      <button class="btn" type="button" onclick={() => (updateFor = null)}>{$tStore('common.cancel')}</button>
      <button class="btn primary" type="button" onclick={startUpdate}>{$tStore('docker.update.submit')}</button>
    </div>
  </Modal>
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
      <button class="btn {a.danger ? 'danger' : 'primary'}" type="button" disabled={askBusy} onclick={confirmAsk}>{a.label}</button>
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
    min-width: 150px;
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

  .checkbar {
    gap: 8px;
  }

  .reg {
    white-space: nowrap;
  }

  tr.hl td {
    background: color-mix(in srgb, var(--accent) 9%, transparent);
  }

  .grp {
    border: 2px solid var(--line);
  }

  .grp + .grp {
    margin-top: 4px;
  }

  .grp-h {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px 12px;
    padding: 8px 12px;
    background: var(--seg-off);
  }

  .grp-toggle {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    padding: 0;
    border: 0;
    background: none;
    color: inherit;
    font: inherit;
    cursor: pointer;
  }

  .grp-toggle h4 {
    margin: 0;
    font-family: var(--font-display);
    font-weight: 400;
    font-size: 16px;
  }

  .grp-toggle:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }

  .grp-sum {
    flex: 1 1 200px;
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .grp-acts {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
    margin-left: auto;
  }

  .grp-acts :global(.btn) {
    display: inline-flex;
    align-items: center;
    height: 30px;
    padding-block: 0;
    font-size: 13px;
  }

  .grp-acts .badge {
    display: inline-flex;
    align-items: center;
  }

  .caret {
    display: inline-block;
    width: 0;
    height: 0;
    border-left: 5px solid transparent;
    border-right: 5px solid transparent;
    border-top: 6px solid currentColor;
    transition: transform 0.15s;
  }

  .grp.closed .caret {
    transform: rotate(-90deg);
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

  /* Medium and narrow screens: drop ports and RAM, let the row actions wrap into two lines. */
  @media (max-width: 1000px) {
    .hide-sm {
      display: none;
    }

    table.grid {
      min-width: 0;
    }

  }

  /* Phones: containers and images as small blocks. */
  @media (max-width: 600px) {
    table.grid {
      min-width: 0;
    }

    table.grid,
    table.grid tbody {
      display: block;
    }

    table.grid thead {
      position: absolute;
      width: 1px;
      height: 1px;
      overflow: hidden;
      clip: rect(0 0 0 0);
    }

    table.grid tr {
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      gap: 6px 12px;
      padding: 8px 4px;
      border-bottom: 2px dashed var(--seg-off);
    }

    table.grid td {
      padding: 0;
      border: 0;
    }

    table.grid td.name {
      flex: 1 0 100%;
      min-width: 0;
    }

    table.grid td.num {
      text-align: left;
    }

    table.grid td[data-label]::before {
      content: attr(data-label) ' ';
      color: var(--muted);
    }

    table.grid td.acts {
      flex: none;
      width: auto;
      margin-left: auto;
    }
  }
</style>
