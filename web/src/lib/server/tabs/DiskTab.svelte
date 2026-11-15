<script lang="ts">
  import { untrack } from 'svelte';
  import { api, ApiError, type DiskCleanItem, type DiskFS, type DiskNode, type DiskScan, type DiskState, type ServerDetail } from '../../../api';
  import { locale, t, tr as T } from '../../../i18n';
  import { showToast } from '../../toast';
  import PieChart, { type PiePart } from '../../ui/PieChart.svelte';
  import { formatBytes, formatDateTime } from '../format';
  import { unavailableKind } from './svc';

  let { server }: { server: ServerDetail } = $props();

  type LoadState = 'loading' | 'ready' | 'offline' | 'disabled' | 'error';

  const POLL_MS = 60_000;
  const CATEGORY_COLOR: Record<string, string> = {
    sys: 'var(--c1)',
    docker: 'var(--c2)',
    logs: 'var(--c3)',
    db: 'var(--c4)',
    tmp: 'var(--c5)',
    home: 'var(--c6)',
    other: 'var(--muted)',
    reserved: 'var(--line)',
    free: 'var(--seg-off)',
  };

  let info = $state<DiskState | null>(null);
  let loadState = $state<LoadState>('loading');
  let seq = 0;

  async function load(id: string): Promise<void> {
    const req = ++seq;
    try {
      const data = await api.disk(id);
      if (req !== seq) return;
      info = data;
      loadState = 'ready';
    } catch (e) {
      if (req !== seq) return;
      loadState = unavailableKind(e) ?? 'error';
    }
  }

  $effect(() => {
    const id = server.id;
    untrack(() => {
      info = null;
      scan = null;
      path = [];
      loadState = 'loading';
      void load(id);
    });
    const timer = setInterval(() => void load(id), POLL_MS);
    return () => clearInterval(timer);
  });

  const unreachable = $derived(!server.online || loadState === 'offline' || loadState === 'disabled');
  const filesystems = $derived(info?.filesystems ?? []);

  const tx = (key: string, vars?: Record<string, string | number>): string => t(key, undefined, vars);

  // ---- filesystems ----

  function usedColor(pct: number): string {
    return pct >= 85 ? 'var(--crit)' : pct >= 70 ? 'var(--warn)' : 'var(--c1)';
  }

  function fsParts(f: DiskFS): PiePart[] {
    return [
      { id: 'used', name: tx('disk.used'), value: f.used, color: usedColor(f.pct) },
      { id: 'free', name: tx('disk.free'), value: f.avail, color: 'var(--seg-off)' },
    ];
  }

  function days(n: number): string {
    return n >= 365 ? tx('disk.moreThanYear') : tx('disk.inDays', { n: Math.max(1, Math.round(n)) });
  }

  function forecastTone(f: DiskFS): string {
    return f.days_left > 0 && f.days_left <= 14 ? 'crit' : f.days_left > 0 && f.days_left <= 60 ? 'warn' : 'muted';
  }

  function bars(trend: number[]): number[] {
    if (trend.length === 0) return [];
    const mx = Math.max(...trend);
    const mn = Math.min(...trend);
    return trend.map((v) => 18 + ((v - mn) / (mx - mn || 1)) * 82);
  }

  // ---- what takes the space ----
  let pick = $state<string>('');
  let scan = $state<DiskScan | null>(null);
  let scanning = $state(false);
  let scanError = $state<string | null>(null);
  let path = $state<DiskNode[]>([]);

  const mount = $derived(filesystems.find((f) => f.mount === pick)?.mount ?? filesystems[0]?.mount ?? '');
  const mountFs = $derived(filesystems.find((f) => f.mount === mount) ?? null);
  const scanFor = $derived(scan && scan.mount === mount ? scan : null);

  const categoryParts = $derived<PiePart[]>(
    (scanFor?.categories ?? []).filter((c) => c.size > 0).map((c) => ({ id: c.id, name: tx(`disk.cat.${c.id}`), value: c.size, color: CATEGORY_COLOR[c.id] ?? 'var(--muted)' })),
  );

  async function runScan(force: boolean): Promise<void> {
    if (scanning || !mount) return;
    const id = server.id;
    const target = mount;
    scanning = true;
    scanError = null;
    try {
      const res = await api.diskScan(id, target, force);
      if (id !== server.id) return;
      scan = res;
      path = [res.tree];
    } catch (e) {
      scanError = e instanceof ApiError && e.serverMessage ? e.serverMessage : tx('common.genericError');
    } finally {
      scanning = false;
    }
  }

  function chooseMount(m: string): void {
    pick = m;
    path = scan && scan.mount === m ? [scan.tree] : [];
  }

  const current = $derived(path.length > 0 ? path[path.length - 1] : null);
  const rows = $derived.by(() => {
    const node = current;
    if (!node) return [];
    const kids = [...(node.children ?? [])].sort((a, b) => b.size - a.size);
    const rest = node.size - kids.reduce((n, k) => n + k.size, 0);
    const list = kids.map((k) => ({ node: k, name: k.name, size: k.size, link: (k.children?.length ?? 0) > 0 }));
    if (rest > 0) list.push({ node: null as unknown as DiskNode, name: tx('disk.dirs.other'), size: rest, link: false });
    return list;
  });
  const biggest = $derived(rows.reduce((n, r) => Math.max(n, r.size), 1));

  // ---- cleanup ----
  let confirm = $state<DiskCleanItem | null>(null);
  let cleaning = $state(false);
  let cleanError = $state<string | null>(null);

  const reclaimable = $derived((info?.cleanup ?? []).reduce((n, c) => n + c.frees, 0));

  function command(c: DiskCleanItem): string {
    switch (c.kind) {
      case 'journal':
        return 'journalctl --vacuum-size=500M';
      case 'apt':
        return 'apt-get clean';
      case 'docker-images':
        return 'docker image prune -a';
      case 'docker-build':
        return 'docker builder prune';
      default:
        return `apt-get purge -y ${(c.items ?? []).join(' ')}`;
    }
  }

  async function runClean(): Promise<void> {
    const c = confirm;
    if (!c || cleaning) return;
    cleaning = true;
    cleanError = null;
    try {
      const res = await api.diskClean(server.id, c.kind);
      confirm = null;
      showToast(tx('disk.clean.done', { size: formatBytes(res.freed) }));
      await load(server.id);
    } catch (e) {
      cleanError = e instanceof ApiError && e.serverMessage ? e.serverMessage : tx('common.genericError');
    } finally {
      cleaning = false;
    }
  }
</script>

<div class="disk">
  {#if unreachable}
    <p class="notice" role="status">{$T(loadState === 'disabled' ? 'svc.disabled' : 'svc.offline')}</p>
  {/if}
  {#if loadState === 'loading'}
    <p class="muted">{$T('common.loading')}</p>
  {:else if loadState === 'error'}
    <p class="error" role="alert">{$T('disk.loadFailed')}</p>
  {/if}

  {#if info}
    <div class="fss">
      {#each filesystems as f (f.mount)}
        <section class="card stack">
          <h3>
            {$T('disk.partition')} <span class="mono">{f.mount}</span>
            <span class="muted small sub">{f.device.replace('/dev/', '')} · {f.fs}</span>
          </h3>
          <PieChart parts={fsParts(f)} size={150} />
          <div class="stack">
            <p class="small muted">
              {formatBytes(f.used)} / {formatBytes(f.total)} · {Math.round(f.pct)}%
              {#if f.inodes_total > 0}· {$T('disk.inodes', { pct: Math.round((f.inodes_used / f.inodes_total) * 100) })}{/if}
            </p>
            {#if f.history_days >= 0.5 && f.per_day !== 0}
              <p class="small muted">
                {f.per_day > 0 ? $T('disk.growth') : $T('disk.shrink')}
                <b class="ink">{$T('disk.perDay', { size: formatBytes(Math.abs(f.per_day)) })}</b>
              </p>
            {:else}
              <p class="small muted">{$T('disk.collecting')}</p>
            {/if}
            {#if f.trend.length > 1}
              <div class="spark" aria-label={$T('disk.trend')}>
                {#each bars(f.trend) as h, i (i)}<i style:height="{h}%"></i>{/each}
              </div>
            {/if}
            {#if f.days_left > 0}
              <p class="small"><span class="badge {forecastTone(f)}">{$T('disk.fillsIn', { when: days(f.days_left) })}</span></p>
            {/if}
          </div>
        </section>
      {/each}
    </div>

    <section class="card stack" aria-labelledby="disk-cat">
      <div class="row between">
        <h3 id="disk-cat">
          {$T('disk.what')}
          {#if mountFs}<span class="muted small sub">{$T('disk.ofTotal', { used: formatBytes(mountFs.used), total: formatBytes(mountFs.total) })}</span>{/if}
        </h3>
        <div class="row">
          {#if filesystems.length > 1}
            <div class="row" role="group" aria-label={$T('disk.partition')}>
              {#each filesystems as f (f.mount)}
                <button class="chip" type="button" aria-pressed={mount === f.mount} onclick={() => chooseMount(f.mount)}>{f.mount}</button>
              {/each}
            </div>
          {/if}
          <button class="btn" type="button" disabled={scanning || unreachable || !mount} onclick={() => void runScan(scanFor !== null)}>
            {scanning ? $T('disk.scanning') : scanFor ? $T('disk.rescan') : $T('disk.scan')}
          </button>
        </div>
      </div>
      {#if scanError}<p class="error" role="alert">{scanError}</p>{/if}
      {#if !scanFor}
        <p class="muted">{scanning ? $T('disk.scanningHint') : $T('disk.scanHint')}</p>
      {:else}
        <p class="small muted">
          {$T('disk.scannedAt', { time: formatDateTime(scanFor.scanned_at, $locale) })}
          {#if scanFor.partial}· <span class="warn">{$T('disk.partial')}</span>{/if}
        </p>
        <PieChart parts={categoryParts} size={250} showShare />
      {/if}
    </section>

    {#if scanFor && current}
      <section class="card stack" aria-labelledby="disk-dirs">
        <div class="row between">
          <h3 id="disk-dirs">{$T('disk.dirs')}</h3>
          <div class="crumbs">
            {#each path as n, i (i)}
              <button type="button" onclick={() => (path = path.slice(0, i + 1))}>{n.name}</button>
              {#if i < path.length - 1}<span class="muted">›</span>{/if}
            {/each}
          </div>
        </div>
        <div class="rowsl">
          {#each rows as r (r.name)}
            {#if r.link}
              <button class="drow link" type="button" onclick={() => (path = [...path, r.node])}>
                <span class="nm mono">{r.name} <span class="muted">›</span></span>
                <span class="track"><i style:width="{(r.size / biggest) * 100}%"></i></span>
                <span class="sz mono">{formatBytes(r.size)}</span>
              </button>
            {:else}
              <div class="drow">
                <span class="nm mono">{r.name}</span>
                <span class="track"><i style:width="{(r.size / biggest) * 100}%"></i></span>
                <span class="sz mono">{formatBytes(r.size)}</span>
              </div>
            {/if}
          {:else}
            <p class="muted small">{$T('disk.dirs.empty')}</p>
          {/each}
        </div>
        <p class="small muted">{$T('disk.dirs.hint')}</p>
      </section>
    {/if}

    <section class="card stack" aria-labelledby="disk-clean">
      <div class="row between">
        <h3 id="disk-clean">{$T('disk.clean')}</h3>
        {#if reclaimable > 0}<p class="small">{$T('disk.clean.upTo', { size: formatBytes(reclaimable) })}</p>{/if}
      </div>
      {#if info.cleanup.length === 0}
        <p class="muted">{$T('disk.clean.none')}</p>
      {:else}
        <div class="clean">
          {#each info.cleanup as c (c.kind)}
            <div class="cc">
              <span class="muted small">{$T(`disk.clean.${c.kind}`)}</span>
              <span class="big">{formatBytes(c.size)}</span>
              <span class="small muted">{$T(`disk.clean.${c.kind}.text`, { frees: formatBytes(c.frees), n: c.count ?? 0 })}</span>
              <div class="row end">
                <button class="btn small-btn" type="button" disabled={unreachable} onclick={() => { confirm = c; cleanError = null; }}>{$T(`disk.clean.${c.kind}.go`)}</button>
              </div>
            </div>
          {/each}
        </div>
      {/if}
      {#if confirm}
        <div class="confirm stack" role="alertdialog" aria-label={$T(`disk.clean.${confirm.kind}`)}>
          <p>{$T('disk.clean.confirm', { what: $T(`disk.clean.${confirm.kind}`), size: formatBytes(confirm.frees) })}</p>
          <pre class="cmd mono">{command(confirm)}</pre>
          {#if confirm.items && confirm.items.length > 0 && confirm.kind !== 'kernels'}
            <ul class="list mono small">
              {#each confirm.items as it (it)}<li>{it}</li>{/each}
            </ul>
          {/if}
          <p class="small muted">{$T('disk.clean.note')}</p>
          {#if cleanError}<p class="error" role="alert">{cleanError}</p>{/if}
          <div class="row end">
            <button class="btn" type="button" disabled={cleaning} onclick={() => (confirm = null)}>{$T('common.cancel')}</button>
            <button class="btn danger" type="button" disabled={cleaning} onclick={() => void runClean()}>{cleaning ? $T('disk.cleaning') : $T('disk.clean.run')}</button>
          </div>
        </div>
      {/if}
    </section>
  {/if}
</div>

<style>
  .disk {
    display: flex;
    flex-direction: column;
    gap: 14px;
    min-width: 0;
  }

  h3,
  p {
    margin: 0;
  }

  h3 {
    font-size: 16px;
    color: var(--muted);
    text-transform: uppercase;
    letter-spacing: 0.06em;
  }

  .sub {
    text-transform: none;
    letter-spacing: 0;
    margin-left: 6px;
  }

  .fss {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(340px, 1fr));
    gap: 14px;
  }

  .between {
    justify-content: space-between;
    flex-wrap: wrap;
  }

  .notice {
    padding: 8px 12px;
    border: 2px solid var(--warn);
    color: var(--warn);
  }

  .error {
    color: var(--crit);
  }

  .warn {
    color: var(--warn);
  }

  .ink {
    color: var(--ink);
  }

  .spark {
    display: flex;
    align-items: flex-end;
    gap: 2px;
    height: 34px;
  }

  .spark i {
    flex: 1;
    min-height: 2px;
    background: var(--c1);
  }

  .spark i:last-child {
    background: var(--warn);
  }

  .badge {
    display: inline-block;
    padding: 1px 6px;
    border: 2px solid var(--muted);
    color: var(--muted);
    font-size: 12px;
  }

  .badge.warn {
    border-color: var(--warn);
    color: var(--warn);
  }

  .badge.crit {
    border-color: var(--crit);
    color: var(--crit);
  }

  .crumbs {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 4px;
  }

  .crumbs button {
    padding: 0 2px;
    border: 0;
    background: none;
    color: var(--accent);
    font: inherit;
    text-decoration: underline;
    cursor: pointer;
  }

  .rowsl {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .drow {
    display: grid;
    grid-template-columns: minmax(120px, 240px) 1fr 90px;
    gap: 12px;
    align-items: center;
    width: 100%;
    padding: 6px 8px;
    border: 0;
    background: none;
    color: inherit;
    font: inherit;
    text-align: left;
    cursor: default;
  }

  .drow.link {
    cursor: pointer;
  }

  .drow.link:hover,
  .drow.link:focus-visible {
    background: color-mix(in srgb, var(--accent) 9%, transparent);
  }

  .nm {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .track {
    height: 14px;
    background: var(--seg-off);
  }

  .track i {
    display: block;
    height: 100%;
    background: var(--c1);
  }

  .sz {
    text-align: right;
  }

  .clean {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
    gap: 12px;
  }

  .cc {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 12px;
    border: 2px solid var(--line);
  }

  .cc .big {
    font-family: var(--font-display);
    font-size: 24px;
  }

  .small-btn {
    display: inline-flex;
    align-items: center;
    height: 30px;
    padding: 0 10px;
    font-size: 13px;
  }

  .confirm {
    padding: 12px;
    border: 2px solid var(--crit);
  }

  .cmd {
    margin: 0;
    padding: 10px;
    border: 2px solid var(--line);
    background: var(--bg);
    font-size: 12px;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }

  .list {
    margin: 0;
    padding-left: 18px;
    max-height: 160px;
    overflow: auto;
  }

  @media (max-width: 700px) {
    .drow {
      grid-template-columns: 90px 1fr 70px;
    }
  }
</style>
