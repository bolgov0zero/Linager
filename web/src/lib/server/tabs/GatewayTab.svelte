<script lang="ts">
  import { untrack } from 'svelte';
  import { api, type GwConfigEdit, type GwDevice, type GwNet, type GwNetEdit, type GwState, type GwType, type ServerDetail } from '../../../api';
  import { locale, t, tr as T } from '../../../i18n';
  import { pluralForm } from '../../dashboard/format';
  import Fold from '../../ui/Fold.svelte';
  import Segments from '../../ui/Segments.svelte';
  import Select from '../../ui/Select.svelte';
  import { isPhone } from '../../ui/viewport';
  import { showToast } from '../../toast';
  import { formatDateTime } from '../format';
  import { hostCount, ipCompare, MAX_NETS, nameSrcKey, portTag } from '../gatewayNet';
  import GatewayDeviceModal from './GatewayDeviceModal.svelte';
  import GatewayEnableModal from './GatewayEnableModal.svelte';
  import GatewayNetModal from './GatewayNetModal.svelte';
  import SecAcc from './SecAcc.svelte';
  import { errorText, unavailableKind } from './svc';
  import './phone-c.css';

  let { server, canWrite = true }: { server: ServerDetail; canWrite?: boolean } = $props();

  type LoadState = 'loading' | 'ready' | 'offline' | 'disabled' | 'error';
  type Filter = 'all' | 'online' | 'offline' | 'new' | 'noname';
  /** How often the state is reloaded: fast while a pass runs or a network waits for its first result, slow otherwise. */
  const FAST_MS = 3000;
  const SLOW_MS = 15_000;
  const INTERVALS = [60, 300, 900, 1800];
  const PHASES = ['arp', 'ping', 'ports', 'names'];
  /** The device types in the order of the type filter. */
  const TYPES: readonly GwType[] = ['router', 'hypervisor', 'nas', 'printer', 'camera', 'phone', 'tv', 'speaker', 'iot', 'windows', 'server'];

  const tx = (k: string, v?: Record<string, string | number>) => t(k, undefined, v);
  const nowSec = () => Math.floor(Date.now() / 1000);

  let st = $state<GwState | null>(null);
  let loadState = $state<LoadState>('loading');
  let now = $state(nowSec());
  let saving = $state(false);
  let scanning = $state(false);
  let seq = 0;

  // ---- loading and polling ----
  async function load(id: string): Promise<void> {
    const req = ++seq;
    try {
      const res = await api.gateway(id);
      if (req !== seq) return;
      st = res;
      now = nowSec();
      loadState = 'ready';
    } catch (e) {
      if (req !== seq) return;
      // a failed refresh keeps what is shown; only the first load turns into an error
      if (!st) loadState = unavailableKind(e) ?? 'error';
    }
  }

  function wantsFast(s: GwState | null): boolean {
    return !!s && (!!s.live?.running || (s.enabled && s.nets.some((n) => n.on && !n.last)));
  }

  $effect(() => {
    const id = server.id;
    untrack(() => {
      st = null;
      loadState = 'loading';
    });
    let stopped = false;
    let timer: ReturnType<typeof setTimeout> | undefined;

    const schedule = (): void => {
      clearTimeout(timer);
      if (stopped || document.hidden) return;
      timer = setTimeout(() => void tick(), wantsFast(st) ? FAST_MS : SLOW_MS);
    };
    const tick = async (): Promise<void> => {
      await load(id);
      schedule();
    };
    const onVisible = (): void => {
      if (document.hidden) clearTimeout(timer);
      else void tick();
    };

    document.addEventListener('visibilitychange', onVisible);
    untrack(() => void tick());
    return () => {
      stopped = true;
      clearTimeout(timer);
      document.removeEventListener('visibilitychange', onVisible);
    };
  });

  // ---- saving: every change is a full save of the edited configuration ----
  const toEdit = (n: GwNet): GwNetEdit => ({ cidr: n.cidr, on: n.on, ports: n.ports, names: n.names, confirm_public: n.public });

  /** Replaces the configuration; resolves with an error text, or null when it worked. */
  async function saveConfig(patch: Partial<Omit<GwConfigEdit, 'confirm'>>): Promise<string | null> {
    const s = st;
    if (!s || saving) return null;
    const cfg: GwConfigEdit = { enabled: s.enabled, interval_s: s.interval_s, nets: s.nets.map(toEdit), ...patch, confirm: true };
    saving = true;
    seq++; // an older refresh must not overwrite the result
    try {
      const res = await api.gatewaySave(server.id, cfg);
      seq++;
      st = res;
      now = nowSec();
      loadState = 'ready';
      void load(server.id);
      return null;
    } catch (e) {
      return errorText(e, tx);
    } finally {
      saving = false;
    }
  }

  /** A change made with a control of the tab: an error comes as a toast. */
  async function run(patch: Partial<Omit<GwConfigEdit, 'confirm'>>, done?: string): Promise<boolean> {
    const err = await saveConfig(patch);
    if (err) showToast(err);
    else if (done) showToast(done);
    return err === null;
  }

  // ---- derived state ----
  const nets = $derived(st?.nets ?? []);
  const devices = $derived(st?.devices ?? []);
  const live = $derived(st?.live);
  const ownNets = $derived(live?.own_nets ?? []);
  /** Nothing was ever scanned: the intro is shown. */
  const intro = $derived(!!st && !st.enabled && nets.every((n) => !n.last) && devices.length === 0);
  /** Scanning is on, but no network has a result yet. */
  const first = $derived(!!st && st.enabled && nets.length > 0 && nets.every((n) => !n.last));
  const sums = $derived({
    found: devices.length,
    online: devices.filter((d) => d.online).length,
    fresh: devices.filter((d) => d.is_new).length,
    excluded: devices.filter((d) => d.excluded).length,
  });
  const lastPassAt = $derived(live?.last_pass_at ?? Math.max(0, ...nets.map((n) => n.last?.finished_at ?? 0)));
  const noAnswer = $derived(nets.filter((n) => n.on && n.last && !n.last.error && n.last.answered === 0));
  const atLimit = $derived(nets.length >= MAX_NETS);

  const Section = $derived($isPhone ? SecAcc : Fold);
  let fold = $state({ nets: true, devs: true, ev: false });

  let intervalSel = $state(300);
  $effect(() => {
    if (st) intervalSel = st.interval_s;
  });
  const intervalOptions = $derived(
    (st && !INTERVALS.includes(st.interval_s) ? [...INTERVALS, st.interval_s].sort((a, b) => a - b) : INTERVALS).map((v) => ({
      value: v,
      label: INTERVALS.includes(v) ? $T(`gw.int.${v}`) : $T('gw.int.custom', { n: Math.round(v / 60) }),
    })),
  );

  // ---- text helpers ----
  function agoText(ts: number): string {
    const d = Math.max(0, now - ts);
    if (d < 60) return $T('gw.ago.now');
    if (d < 3600) return $T('gw.ago.m', { n: Math.floor(d / 60) });
    if (d < 86400) return $T('gw.ago.h', { n: Math.floor(d / 3600) });
    return $T('gw.ago.d', { n: Math.floor(d / 86400) });
  }

  function inText(ts: number): string {
    const d = ts - now;
    if (d <= 5) return $T('gw.in.soon');
    if (d < 60) return $T('gw.in.s', { n: d });
    if (d < 3600) return $T('gw.in.m', { n: Math.ceil(d / 60) });
    return $T('gw.in.h', { n: Math.ceil(d / 3600) });
  }

  const phaseText = (p: string | undefined): string => (p && PHASES.includes(p) ? $T(`gw.phase.${p}`) : (p ?? ''));

  const statusText = $derived.by(() => {
    if (!st) return '';
    if (!st.enabled) return $T('gw.status.stopped');
    const lv = st.live;
    if (first) {
      return lv?.running && lv.index
        ? $T('gw.status.firstProgress', { n: lv.index, m: lv.total ?? nets.length, phase: phaseText(lv.phase) })
        : $T('gw.status.firstWait');
    }
    if (lv?.running) {
      return lv.index ? $T('gw.status.running', { n: lv.index, m: lv.total ?? nets.length, phase: phaseText(lv.phase) }) : $T('gw.status.runningShort');
    }
    const parts = [lastPassAt > 0 ? $T('gw.status.last', { ago: agoText(lastPassAt) }) : $T('gw.status.never')];
    if (lv?.next_pass_at) parts.push($T('gw.status.next', { when: inText(lv.next_pass_at) }));
    return parts.join(' · ');
  });

  const via = (n: GwNet): string => (!n.own && n.via ? $T('gw.nets.via', { via: n.via }) : (n.iface ?? ''));
  const methods = (n: GwNet): string =>
    [n.own ? $T('gw.nets.m.arp') : '', $T('gw.nets.m.ping'), n.ports ? $T('gw.nets.m.ports') : '', n.names ? $T('gw.nets.m.names') : ''].filter(Boolean).join(', ');
  const hostsOf = (n: GwNet): number => hostCount(Number(n.cidr.split('/')[1]) || 24);

  function netResult(n: GwNet): { text: string; tone: 'ok' | 'warn' | 'crit' | 'acc' | 'muted' } {
    if (n.last) {
      if (n.last.error) return { text: $T('gw.nets.error', { error: n.last.error }), tone: 'crit' };
      const text = $T('gw.nets.answered', { n: n.last.answered, m: hostsOf(n) });
      return { text, tone: n.last.answered === 0 && n.on ? 'warn' : 'ok' };
    }
    if (!n.on) return { text: $T('gw.nets.off'), tone: 'muted' };
    if (live?.running) return live.cidr === n.cidr ? { text: $T('gw.nets.running'), tone: 'acc' } : { text: $T('gw.nets.queued'), tone: 'muted' };
    return { text: $T('gw.nets.waiting'), tone: 'muted' };
  }

  function originOf(n: GwNet): { text: string; cls: string } {
    if (n.own) return { text: $T('gw.nets.origin.own'), cls: '' };
    if (n.public) return { text: $T('gw.nets.origin.public'), cls: 'warn' };
    return { text: $T('gw.nets.origin.manual'), cls: 'acc' };
  }

  const nameOf = (d: GwDevice): string => d.own_name || d.name || '';
  function srcOf(d: GwDevice): string {
    if (d.own_name) return $T('gw.src.own');
    const key = nameSrcKey(d.name_src);
    return key ? $T(key) : (d.name_src ?? '');
  }
  const viaLabel = (v: string): string => (['arp', 'ping', 'ports'].includes(v) ? $T(`gw.via.${v}`) : v);
  const nowOrAt = (d: GwDevice): string => (d.online ? $T('gw.dev.justNow') : formatDateTime(d.last_seen, $locale));

  // ---- the list of devices ----
  let query = $state('');
  let filter = $state<Filter>('all');
  let netFilter = $state('all');
  let typeFilter = $state('all');
  let showEx = $state(false);

  const filterOptions = $derived<{ value: Filter; label: string }[]>([
    { value: 'all', label: $T('gw.f.all') },
    { value: 'online', label: $T('gw.f.online') },
    { value: 'offline', label: $T('gw.f.offline') },
    { value: 'new', label: $T('gw.f.new') },
    { value: 'noname', label: $T('gw.f.noname') },
  ]);
  const netOptions = $derived([
    { value: 'all', label: $T('gw.dev.allNets') },
    // networks that were deleted keep their devices in the list
    ...[...new Set([...nets.map((n) => n.cidr), ...devices.map((d) => d.net)])].map((c) => ({ value: c, label: c })),
  ]);

  // only the types that are present among the devices are offered
  const typeOptions = $derived([
    { value: 'all', label: $T('gw.filter.type') },
    ...TYPES.filter((k) => devices.some((d) => d.type === k)).map((k) => ({ value: k, label: $T(`gw.type.${k}`) })),
  ]);

  const rows = $derived.by(() => {
    const q = query.trim().toLowerCase();
    return devices
      .filter((d) => {
        if (d.excluded && !showEx) return false;
        if (filter === 'online' && !d.online) return false;
        if (filter === 'offline' && d.online) return false;
        if (filter === 'new' && !d.is_new) return false;
        if (filter === 'noname' && nameOf(d)) return false;
        if (netFilter !== 'all' && d.net !== netFilter) return false;
        if (typeFilter !== 'all' && d.type !== typeFilter) return false;
        if (q && !`${d.own_name ?? ''} ${d.name ?? ''} ${d.ip} ${d.mac ?? ''} ${d.vendor ?? ''}`.toLowerCase().includes(q)) return false;
        return true;
      })
      .sort((a, b) => ipCompare(a.ip, b.ip));
  });

  const events = $derived([...(st?.events ?? [])].sort((a, b) => b.ts - a.ts || b.id - a.id));

  // ---- actions ----
  let enabling = $state(false);
  let addingNet = $state(false);
  let editing = $state<GwDevice | null>(null);

  function turn(): void {
    if (!st || saving || !canWrite) return;
    if (st.enabled) {
      void run({ enabled: false }, tx('gw.sw.offDone'));
    } else if (nets.length === 0) {
      enabling = true;
    } else {
      void run({ enabled: true }, tx('gw.sw.onDone'));
    }
  }

  async function enable(picked: GwNetEdit[]): Promise<string | null> {
    const err = await saveConfig({ enabled: true, interval_s: 300, nets: picked });
    if (!err) showToast(tx('gw.en.done'));
    return err;
  }

  async function addNets(items: GwNetEdit[]): Promise<string | null> {
    const s = st;
    if (!s) return null;
    const err = await saveConfig({ nets: [...s.nets.map(toEdit), ...items] });
    if (!err) showToast(items.length === 1 ? tx('gw.net.added') : tx('gw.net.addedMany', { parts: tx(`gw.net.n24.${pluralForm($locale, items.length)}`, { n: items.length }) }));
    return err;
  }

  function toggleNet(n: GwNet): void {
    if (saving || !canWrite) return;
    void run({ nets: nets.map((x) => toEdit(x.cidr === n.cidr ? { ...x, on: !x.on } : x)) });
  }

  function removeNet(n: GwNet): void {
    if (saving || !canWrite) return;
    void run({ nets: nets.filter((x) => x.cidr !== n.cidr).map(toEdit) }, tx('gw.nets.deleted', { cidr: n.cidr }));
  }

  async function changeInterval(v: number): Promise<void> {
    if (!(await run({ interval_s: v }, tx('gw.int.done', { value: tx(`gw.int.${v}`) })))) intervalSel = st?.interval_s ?? v;
  }

  async function scanNow(): Promise<void> {
    if (scanning) return;
    scanning = true;
    try {
      await api.gatewayScan(server.id);
      showToast(tx('gw.scan.done'));
      void load(server.id);
    } catch (e) {
      showToast(errorText(e, tx));
    } finally {
      scanning = false;
    }
  }

  async function saveDevice(d: GwDevice, patch: { own_name?: string; excluded?: boolean }): Promise<string | null> {
    try {
      await api.gatewayDevice(server.id, d.id, patch);
      showToast(tx('gw.dlg.saved'));
      void load(server.id);
      return null;
    } catch (e) {
      return errorText(e, tx);
    }
  }

  const netOwn = (d: GwDevice): boolean => nets.find((n) => n.cidr === d.net)?.own ?? true;
  const canScan = $derived(canWrite && !!st?.enabled && !!st.agent_online && !first && !live?.running && !scanning);
</script>

{#snippet nameCell(d: GwDevice)}
  {#if nameOf(d)}
    <span class="nname">{nameOf(d)}</span>{#if srcOf(d)}<span class="src">{srcOf(d)}</span>{/if}
  {:else}
    <span class="muted">{$T('gw.dev.noName')}</span>
  {/if}
{/snippet}

{#snippet badges(d: GwDevice)}
  {#if d.type}<span class="gw-tag type">{$T(`gw.type.${d.type}`)}</span>{/if}
  {#if d.known_server}<span class="gw-tag acc">{$T('gw.badge.agent', { name: d.known_server })}</span>{/if}
  {#if d.is_new}<span class="gw-tag warn">{$T('gw.badge.new')}</span>{/if}
  {#if d.excluded}<span class="gw-tag">{$T('gw.badge.excluded')}</span>{/if}
  {#if d.hints?.includes('debian') && !d.known_server}<span class="gw-tag">{$T('gw.badge.debian')}</span>{/if}
{/snippet}

{#snippet nowCell(d: GwDevice)}
  {#if d.online}
    <span class="tone-ok"><span class="gw-dot"></span>{$T('gw.dev.online')}</span>
  {:else}
    <span class="muted"><span class="gw-dot"></span>{$T('gw.dev.offline')}</span>
  {/if}
{/snippet}

{#snippet netSwitch(n: GwNet)}
  <button
    class="gw-sw"
    class:on={n.on}
    type="button"
    role="switch"
    aria-checked={n.on}
    aria-label={$T('gw.nets.scanLabel', { cidr: n.cidr })}
    disabled={!canWrite || saving}
    onclick={() => toggleNet(n)}><i></i></button
  >
{/snippet}

{#snippet netsBody()}
  {#if $isPhone}
    <ul class="pc-list">
      {#each nets as n (n.cidr)}
        {@const res = netResult(n)}
        {@const org = originOf(n)}
        <li class="pc-li" class:wn={res.tone === 'warn' || res.tone === 'crit'}>
          <div class="pc-mn">
            <div class="pc-l1"><b class="mono">{n.cidr}</b><span class="pc-tag {org.cls === 'acc' ? 'accent' : org.cls || 'muted'}">{org.text}</span></div>
            <div class="pc-l2">{via(n)}{n.vlan ? ` · ${n.vlan}` : ''} · {methods(n)}</div>
            <div class="pc-l2 tone-{res.tone}">{res.text}{n.last ? ` · ${agoText(n.last.finished_at)}` : ''}</div>
          </div>
          <div class="prow">
            {@render netSwitch(n)}
            {#if !n.own && canWrite}<button class="btn sm" type="button" disabled={saving} aria-label={$T('gw.nets.deleteLabel', { cidr: n.cidr })} onclick={() => removeNet(n)}>{$T('gw.nets.delete')}</button>{/if}
          </div>
        </li>
      {:else}
        <li class="pc-li"><span class="muted">—</span></li>
      {/each}
    </ul>
  {:else}
    <div class="table-wrap">
      <table class="tbl net">
        <thead>
          <tr>
            <th scope="col">{$T('gw.nets.col.net')}</th>
            <th scope="col">{$T('gw.nets.col.path')}</th>
            <th scope="col">{$T('gw.nets.col.origin')}</th>
            <th scope="col">{$T('gw.nets.col.methods')}</th>
            <th scope="col">{$T('gw.nets.col.last')}</th>
            <th scope="col">{$T('gw.nets.col.result')}</th>
            <th scope="col">{$T('gw.nets.col.scan')}</th>
            <th scope="col"><span class="sr">{$T('gw.nets.col.actions')}</span></th>
          </tr>
        </thead>
        <tbody>
          {#each nets as n (n.cidr)}
            {@const res = netResult(n)}
            {@const org = originOf(n)}
            <tr class:off={!n.on}>
              <td class="nw"><b class="mono">{n.cidr}</b></td>
              <td>{via(n)}{#if n.vlan} <span class="gw-tag">{n.vlan}</span>{/if}</td>
              <td><span class="gw-tag {org.cls}">{org.text}</span></td>
              <td class="small">
                {methods(n)}
                {#if !n.own}<div class="muted">{$T('gw.nets.noArp')}</div>{/if}
              </td>
              <td class="nw">{n.last ? agoText(n.last.finished_at) : '—'}</td>
              <td class="nw tone-{res.tone}">{res.text}</td>
              <td>{@render netSwitch(n)}</td>
              <td>
                <div class="acts">
                  {#if !n.own && canWrite}<button class="btn sm" type="button" disabled={saving} aria-label={$T('gw.nets.deleteLabel', { cidr: n.cidr })} onclick={() => removeNet(n)}>{$T('gw.nets.delete')}</button>{/if}
                </div>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
  {#each noAnswer as n (n.cidr)}
    <div class="gw-warn">
      {$T('gw.nets.noAnswer', { cidr: n.cidr })}
      {#if !n.own && n.via}{$T('gw.nets.noAnswerVia', { via: $T('gw.nets.via', { via: n.via }) })}{/if}
    </div>
  {/each}
  {#if canWrite}
    <div class="row">
      <button class="btn" type="button" disabled={atLimit || saving} onclick={() => (addingNet = true)}>{$T('gw.nets.add')}</button>
      <span class="hint">{atLimit ? $T('gw.nets.max', { max: MAX_NETS }) : $T('gw.nets.addHint')}</span>
    </div>
  {/if}
{/snippet}

{#snippet devsBody()}
  {#if $isPhone}
    <div class="stack ptools">
      <input class="input" type="search" bind:value={query} placeholder={$T('gw.dev.search')} aria-label={$T('gw.dev.search')} />
      <Select bind:value={filter} options={filterOptions} ariaLabel={$T('gw.dev.filters')} />
      <Select bind:value={typeFilter} options={typeOptions} ariaLabel={$T('gw.filter.type')} />
      <Select bind:value={netFilter} options={netOptions} ariaLabel={$T('gw.dev.netFilter')} />
      <label class="check"><input type="checkbox" bind:checked={showEx} /><span>{$T('gw.dev.showEx')}</span></label>
    </div>
    {#if rows.length === 0}
      <p class="muted empty">{devices.length === 0 ? $T('gw.dev.none') : $T('gw.dev.empty')}</p>
    {:else}
      <div class="pc-list">
        {#each rows as d (d.id)}
          <button class="pc-li" class:dim={d.excluded} type="button" onclick={() => (editing = d)}>
            <span class="pc-mn">
              <span class="pc-l1 nm">{@render nameCell(d)}</span>
              <span class="pc-l2 mono">{d.ip}{d.vendor && !d.random_mac ? ` · ${d.vendor}` : ''}{d.random_mac ? ` · ${$T('gw.dev.randomMac')}` : ''}</span>
              {#if d.type || d.is_new || d.excluded || d.known_server || d.hints?.includes('debian')}<span class="pc-l2 tags">{@render badges(d)}</span>{/if}
              {#if d.ports.length}<span class="pc-l2 tags">{#each d.ports as p (p.port)}<span class="gw-tag">{portTag(p.port)}</span>{/each}</span>{/if}
            </span>
            <span class="pc-r">{@render nowCell(d)}</span>
          </button>
        {/each}
      </div>
    {/if}
  {:else}
    <div class="tools">
      <input class="input search" type="search" bind:value={query} placeholder={$T('gw.dev.search')} aria-label={$T('gw.dev.search')} />
      {#each filterOptions as f (f.value)}
        <button class="chip" type="button" aria-pressed={filter === f.value} onclick={() => (filter = f.value)}>{f.label}</button>
      {/each}
      <div class="netsel"><Select bind:value={typeFilter} options={typeOptions} ariaLabel={$T('gw.filter.type')} /></div>
      <div class="netsel"><Select bind:value={netFilter} options={netOptions} ariaLabel={$T('gw.dev.netFilter')} /></div>
      <label class="check small"><input type="checkbox" bind:checked={showEx} /><span>{$T('gw.dev.showEx')}</span></label>
    </div>
    <div class="table-wrap">
      <table class="tbl dev">
        <thead>
          <tr>
            <th scope="col">{$T('gw.dev.col.device')}</th>
            <th scope="col">{$T('gw.dev.col.addr')}</th>
            <th scope="col">{$T('gw.dev.col.mac')}</th>
            <th scope="col">{$T('gw.dev.col.ports')}</th>
            <th scope="col">{$T('gw.dev.col.via')}</th>
            <th scope="col">{$T('gw.dev.col.seen')}</th>
            <th scope="col">{$T('gw.dev.col.now')}</th>
          </tr>
        </thead>
        <tbody>
          {#each rows as d (d.id)}
            <tr class="d" class:ex={d.excluded} class:off={!d.online} onclick={() => (editing = d)}>
              <td>
                <div class="nm"><button class="link" type="button" onclick={(e) => { e.stopPropagation(); editing = d; }}>{@render nameCell(d)}</button></div>
                <div>{@render badges(d)}</div>
              </td>
              <td class="nw mono">{d.ip}</td>
              <td class="small">
                <div class="mono">{d.mac ?? '—'}</div>
                <div class="muted">{d.random_mac ? $T('gw.dev.randomMac') : (d.vendor ?? '')}</div>
              </td>
              <td class="small">
                {#each d.ports as p (p.port)}<span class="gw-tag">{portTag(p.port)}</span>{:else}<span class="muted">—</span>{/each}
              </td>
              <td class="small">{d.via.map(viaLabel).join(', ')}</td>
              <td class="nw small">
                <div>{$T('gw.dev.firstSeen', { t: formatDateTime(d.first_seen, $locale) })}</div>
                <div class="muted">{$T('gw.dev.lastSeen', { t: nowOrAt(d) })}</div>
              </td>
              <td class="nw">{@render nowCell(d)}</td>
            </tr>
          {:else}
            <tr><td colspan="7" class="muted empty">{devices.length === 0 ? $T('gw.dev.none') : $T('gw.dev.empty')}</td></tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
  <p class="muted small">{$T('gw.dev.legend')}</p>
{/snippet}

{#snippet eventsBody()}
  {#if events.length === 0}
    <p class="muted">{$T('gw.ev.empty')}</p>
  {:else}
    <div class="evs">
      {#each events as e (e.id)}
        <div class="ev">
          <span class="muted">{formatDateTime(e.ts, $locale)}</span>
          <span class={e.kind === 'appeared' ? 'tone-warn' : 'muted'}>{e.kind === 'appeared' ? $T('gw.ev.appeared') : $T('gw.ev.disappeared')}</span>
          <span>{e.name || (e.vendor ? `${e.ip} (${e.vendor})` : e.ip)} <span class="muted">· {e.net}</span></span>
        </div>
      {/each}
    </div>
  {/if}
  <p class="muted small">{$T('gw.ev.legend')}</p>
{/snippet}

<div class="stack gw">
  {#if loadState === 'loading'}
    <p class="muted">{$T('common.loading')}</p>
  {:else if loadState === 'offline'}
    <p class="gw-warn" role="status">{$T('svc.offline')}</p>
  {:else if loadState === 'disabled'}
    <p class="gw-warn" role="status">{$T('svc.disabled')}</p>
  {:else if loadState === 'error' || !st}
    <p class="error" role="alert">{$T('gw.loadFailed')}</p>
  {:else}
    {#if !st.agent_online && !intro}
      <div class="gw-warn" role="status">
        <b>{$T('gw.stale.title')}</b>
        {st.last_data_at ? $T('gw.stale.text', { time: formatDateTime(st.last_data_at, $locale) }) : $T('gw.stale.noTime')}
      </div>
    {/if}

    {#if intro}
      <section class="card stack intro" aria-labelledby="gw-off">
        <h3 id="gw-off" class="cap">{$T('gw.off.title')}</h3>
        <p>{$T('gw.off.text')}</p>
        <p class="muted small">{ownNets.length > 0 ? $T('gw.off.own') : $T('gw.off.ownNone')}</p>
        {#if ownNets.length > 0}
          <div class="ownlist">
            {#each ownNets as o (o.cidr)}
              <div class="gw-msg"><b class="mono">{o.cidr}</b> · {o.iface}{o.vlan ? ` · ${o.vlan}` : ''}</div>
            {/each}
          </div>
        {/if}
        <ul class="bul">
          <li>{$T('gw.off.how')}</li>
          <li>{$T('gw.off.limits')}</li>
          <li>{$T('gw.off.keep')}</li>
        </ul>
        {#if canWrite}
          <div class="row">
            <button class="btn primary" type="button" disabled={!st.agent_online || ownNets.length === 0} onclick={() => (enabling = true)}>{$T('gw.off.enable')}</button>
            {#if !st.agent_online}<span class="hint">{$T('gw.off.needAgent')}</span>{:else if ownNets.length === 0}<span class="hint">{$T('gw.off.noOwn')}</span>{/if}
          </div>
        {/if}
      </section>
    {:else}
      <section class="card stack" aria-label={$T('srv.tab.gateway')}>
        <div class="statusrow">
          <button class="gw-sw" class:on={st.enabled} type="button" role="switch" aria-checked={st.enabled} disabled={!canWrite || saving} onclick={turn}>
            <i></i><b>{st.enabled ? $T('gw.sw.on') : $T('gw.sw.off')}</b>
          </button>
          <div class="grow small muted" role="status">{statusText}</div>
          <div class="row">
            <span class="small muted">{$T('gw.interval')}</span>
            <Select
              size="sm"
              bind:value={intervalSel}
              options={intervalOptions}
              ariaLabel={$T('gw.interval')}
              disabled={!canWrite || !st.enabled || saving}
              onchange={(v) => void changeInterval(v)}
            />
            {#if canWrite}<button class="btn sm" type="button" disabled={!canScan} onclick={() => void scanNow()}>{$T('gw.scan')}</button>{/if}
          </div>
        </div>
        {#if first}<div class="bar"><Segments value={0} busy plain /></div>{/if}
        {#if live && !live.ping_ok}<p class="gw-warn">{$T('gw.status.noPing')}</p>{/if}
        {#if !first}
          <div class="sums">
            <div class="sum"><b>{sums.found}</b><span class="muted small">{$T('gw.sum.found')}</span></div>
            <div class="sum"><b>{sums.online}</b><span class="muted small">{$T('gw.sum.online')}</span></div>
            <div class="sum"><b>{sums.fresh}</b><span class="muted small">{$T('gw.sum.new')}</span></div>
            <div class="sum"><b>{sums.excluded}</b><span class="muted small">{$T('gw.sum.excluded')}</span></div>
          </div>
        {/if}
      </section>

      <Section title={$T('gw.nets')} meta={$T('gw.nets.meta', { n: nets.length, max: MAX_NETS })} bind:open={fold.nets}>
        {#if $isPhone}<section class="card stack">{@render netsBody()}</section>{:else}{@render netsBody()}{/if}
      </Section>

      <Section title={$T('gw.dev')} meta={first ? $T('gw.dev.metaFirst', { n: devices.length }) : $T('gw.dev.meta', { shown: rows.length, total: devices.length })} bind:open={fold.devs}>
        {#if $isPhone}<section class="card stack">{@render devsBody()}</section>{:else}{@render devsBody()}{/if}
      </Section>

      {#if !first}
        <Section title={$T('gw.ev')} meta={String(events.length)} bind:open={fold.ev}>
          {#if $isPhone}<section class="card stack">{@render eventsBody()}</section>{:else}{@render eventsBody()}{/if}
        </Section>
      {/if}
    {/if}
  {/if}
</div>

{#if enabling && st}
  <GatewayEnableModal name={server.name} {ownNets} onclose={() => (enabling = false)} onenable={enable} />
{/if}
{#if addingNet && st}
  <GatewayNetModal {nets} {ownNets} onclose={() => (addingNet = false)} onadd={addNets} />
{/if}
{#if editing}
  {@const dev = editing}
  <GatewayDeviceModal device={dev} netOwn={netOwn(dev)} {canWrite} onclose={() => (editing = null)} onsave={(patch) => saveDevice(dev, patch)} />
{/if}

<style>
  .cap {
    margin: 0;
    font-size: 16px;
    font-weight: 400;
    color: var(--muted);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }

  .statusrow {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 12px 18px;
  }

  .statusrow .grow {
    flex: 1 1 280px;
  }

  .statusrow b {
    font-family: var(--font-display);
    font-weight: 400;
    font-size: 18px;
  }

  .bar {
    max-width: 420px;
  }

  .sums {
    display: grid;
    grid-template-columns: repeat(4, minmax(0, 1fr));
    gap: 12px;
  }

  .sum {
    padding: 10px 12px;
    border: 2px solid var(--line);
  }

  .sum b {
    display: block;
    font-family: var(--font-display);
    font-weight: 400;
    font-size: 26px;
    line-height: 1.1;
  }

  .ownlist {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .bul {
    margin: 0;
    padding-left: 20px;
  }

  .bul li {
    margin: 3px 0;
  }

  .tools {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
  }

  .search {
    flex: 1 1 200px;
    max-width: 320px;
    height: 36px;
    padding-block: 0;
  }

  .netsel {
    width: 170px;
  }

  .table-wrap {
    overflow-x: auto;
  }

  .tbl {
    width: 100%;
    border-collapse: collapse;
    font-size: 13px;
  }

  .tbl.net {
    min-width: 900px;
  }

  .tbl.dev {
    min-width: 960px;
  }

  .tbl th {
    padding: 6px 8px;
    text-align: left;
    font-family: var(--font-display);
    font-weight: 400;
    font-size: 12px;
    color: var(--muted);
    border-bottom: 2px solid var(--line);
    white-space: nowrap;
  }

  .tbl td {
    padding: 8px;
    vertical-align: middle;
    border-bottom: 1px dashed var(--seg-off);
  }

  .tbl tr.d {
    cursor: pointer;
  }

  .tbl tr.d:hover td {
    background: var(--seg-off);
  }

  .tbl tr.ex td {
    opacity: 0.55;
  }

  .tbl tr.off td {
    color: var(--muted);
  }

  .nw {
    white-space: nowrap;
  }

  .nm {
    display: flex;
    align-items: center;
    gap: 8px;
    font-weight: 600;
  }

  .nm :global(.src) {
    font-weight: 400;
    color: var(--muted);
    font-size: 11px;
  }

  .link {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 0;
    background: none;
    border: 0;
    color: inherit;
    cursor: pointer;
    font: inherit;
    font-weight: 600;
    text-align: left;
    white-space: nowrap;
  }

  .link :global(.src) {
    font-weight: 400;
    color: var(--muted);
    font-size: 11px;
  }

  .acts {
    display: flex;
    justify-content: flex-end;
    gap: 6px;
  }

  .tone-ok {
    color: var(--good);
  }

  .tone-warn {
    color: var(--warn);
  }

  .tone-crit {
    color: var(--crit);
  }

  .tone-acc {
    color: var(--accent);
  }

  .tone-muted {
    color: var(--muted);
  }

  .empty {
    padding: 24px;
    text-align: center;
  }

  .evs {
    display: flex;
    flex-direction: column;
  }

  .ev {
    display: grid;
    grid-template-columns: 150px 110px 1fr;
    gap: 4px 12px;
    padding: 6px 0;
    border-bottom: 1px dashed var(--seg-off);
    font-size: 13px;
  }

  .sr {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip: rect(0 0 0 0);
  }

  /* Shared with the dialogs of the tab (they are rendered inside it). */
  :global(.gw-warn),
  :global(.gw-crit) {
    margin: 0;
    padding: 8px 10px;
    border: 2px solid var(--warn);
    color: var(--warn);
    font-size: 12px;
  }

  :global(.gw-crit) {
    border-color: var(--crit);
    color: var(--crit);
  }

  :global(.gw-msg) {
    padding: 6px 8px;
    border: 2px dashed var(--seg-off);
    font-size: 12px;
    overflow-wrap: anywhere;
  }

  :global(.gw-msg.err) {
    border-color: var(--crit);
    color: var(--crit);
  }

  :global(.gw-msg.ok) {
    border-color: var(--good);
    color: var(--good);
  }

  :global(.gw-tag) {
    display: inline-block;
    margin: 1px 3px 1px 0;
    padding: 0 6px;
    border: 1px solid var(--muted);
    color: var(--muted);
    font-size: 12px;
    white-space: nowrap;
  }

  :global(.gw-tag.acc) {
    border-color: var(--accent);
    color: var(--accent);
  }

  :global(.gw-tag.type) {
    border-color: var(--ink);
    color: var(--ink);
  }

  :global(.gw-tag.warn) {
    border-color: var(--warn);
    color: var(--warn);
  }

  :global(.gw-badge) {
    display: inline-block;
    padding: 0 6px;
    border: 2px solid currentColor;
    font-size: 12px;
    line-height: 1.35;
    white-space: nowrap;
  }

  :global(.gw-badge.ok) {
    color: var(--good);
  }

  :global(.gw-badge.muted) {
    color: var(--muted);
  }

  :global(.gw-dot) {
    display: inline-block;
    width: 8px;
    height: 8px;
    margin-right: 6px;
    background: currentColor;
  }

  :global(.gw-sw) {
    display: inline-flex;
    align-items: center;
    gap: 10px;
    padding: 0;
    background: none;
    border: 0;
    color: inherit;
    cursor: pointer;
    font: inherit;
    text-align: left;
  }

  :global(.gw-sw > i) {
    position: relative;
    flex: none;
    width: 44px;
    height: 24px;
    background: var(--seg-off);
    border: 2px solid var(--line);
  }

  :global(.gw-sw > i::after) {
    content: '';
    position: absolute;
    top: 2px;
    left: 2px;
    width: 16px;
    height: 16px;
    background: var(--ink);
  }

  :global(.gw-sw.on > i) {
    background: var(--accent);
  }

  :global(.gw-sw.on > i::after) {
    left: 22px;
    background: var(--surface);
  }

  :global(.gw-sw:disabled) {
    opacity: 0.5;
    cursor: not-allowed;
  }

  :global(.gw-sw:focus-visible > i) {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }

  @media (max-width: 860px) {
    .sums {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
  }

  @media (max-width: 720px) {
    .sums {
      gap: 8px;
    }

    .sum {
      padding: 8px 10px;
      background: var(--surface);
      box-shadow: 3px 3px 0 var(--shade);
    }

    .sum b {
      font-size: 22px;
    }

    .statusrow {
      gap: 10px;
    }

    .statusrow .row {
      width: 100%;
    }

    .ptools {
      gap: 10px;
    }

    .prow {
      display: flex;
      flex-direction: column;
      align-items: flex-end;
      gap: 6px;
    }

    :global(.gw-sw) {
      min-height: 44px;
    }

    .pc-li.dim {
      opacity: 0.55;
    }

    .nm {
      font-weight: 400;
    }

    .tags {
      display: flex;
      flex-wrap: wrap;
      gap: 2px;
    }

    .ev {
      grid-template-columns: 1fr;
    }
  }
</style>
