<script lang="ts">
  import { ApiError, winCall, type ServerDetail, type WinAdapter, type WinNetApply, type WinNetState } from '../../../api';
  import { tr } from '../../../i18n';
  import PixIcon from '../../ui/PixIcon.svelte';
  import Select from '../../ui/Select.svelte';
  import { showToast } from '../../toast';
  import WinConfirm from './WinConfirm.svelte';

  let { server }: { server: ServerDetail } = $props();
  const serverId = $derived(server.id);

  let data = $state<WinNetState | null>(null);
  let loading = $state(true);
  let error = $state('');
  let busy = $state(false);
  let editing = $state<WinAdapter | null>(null);
  let opened = $state<number[]>([]);
  let asking = $state<WinNetApply | null>(null);
  let pending = $state<{ id: string; deadline: number } | null>(null);
  let left = $state(0);

  let fMode = $state<'dhcp' | 'static'>('dhcp');
  const modeOptions = $derived([
    { value: 'dhcp' as const, label: $tr('wn.dhcp') },
    { value: 'static' as const, label: $tr('wn.static') },
  ]);
  let fIp = $state('');
  let fPrefix = $state(24);
  let fGw = $state('');
  let fDns = $state('');

  const msg = (e: unknown) => (e instanceof ApiError && e.serverMessage ? e.serverMessage : $tr('win.error'));

  async function load(): Promise<void> {
    loading = true;
    try {
      data = await winCall<WinNetState>(serverId, 'network', 'state');
      error = '';
      if (data.pending) pending = data.pending;
    } catch (e) {
      error = msg(e);
    } finally {
      loading = false;
    }
  }

  $effect(() => {
    void serverId;
    void load();
  });

  // the countdown of a change that waits for its confirmation
  $effect(() => {
    if (!pending) return;
    const tick = () => {
      left = Math.max(0, pending ? pending.deadline - Math.floor(Date.now() / 1000) : 0);
      if (left === 0) {
        pending = null;
        showToast($tr('wn.reverted'));
        void load();
      }
    };
    tick();
    const t = setInterval(tick, 1000);
    return () => clearInterval(t);
  });

  const toggle = (i: number) => (opened = opened.includes(i) ? opened.filter((x) => x !== i) : [...opened, i]);

  function edit(a: WinAdapter): void {
    editing = a;
    fMode = a.dhcp ? 'dhcp' : 'static';
    const [ip, pl] = (a.ipv4[0] ?? '').split('/');
    fIp = ip ?? '';
    fPrefix = Number(pl) || 24;
    fGw = a.gateway === '0.0.0.0' ? '' : a.gateway;
    fDns = a.dns.join(', ');
  }

  function review(): void {
    if (!editing) return;
    asking = {
      index: editing.index,
      mode: fMode,
      ...(fMode === 'static' ? { ip: fIp.trim(), prefix: fPrefix, gateway: fGw.trim() } : {}),
      dns: fDns.split(/[,\s]+/).filter(Boolean),
    };
  }

  async function apply(): Promise<void> {
    const req = asking;
    asking = null;
    if (!req) return;
    busy = true;
    try {
      const r = await winCall<{ id: string; deadline: number }>(serverId, 'network', 'apply', { ...req }, true);
      pending = { id: r.id, deadline: r.deadline };
      editing = null;
    } catch (e) {
      // the answer may never arrive if the change cut the connection: the agent undoes it by itself
      if (e instanceof ApiError && e.status !== 0 && e.status < 500) showToast(msg(e));
      else showToast($tr('wn.lost'));
    } finally {
      busy = false;
    }
  }

  async function settle(action: 'confirm' | 'rollback'): Promise<void> {
    const p = pending;
    if (!p) return;
    try {
      await winCall(serverId, 'network', action, { id: p.id });
      pending = null;
      showToast($tr(action === 'confirm' ? 'wn.kept' : 'wn.reverted'));
      await load();
    } catch (e) {
      showToast(msg(e));
    }
  }
</script>

<div class="wrap">
  <div class="bar">
    <h3>{$tr('wn.title')}</h3>
    <button class="btn" type="button" onclick={() => void load()}>{$tr('win.refresh')}</button>
  </div>

  {#if pending}
    <div class="warn" role="status">
      <span>{$tr('wn.pending', { n: left })}</span>
      <button class="btn primary" type="button" onclick={() => void settle('confirm')}>{$tr('wn.keep')}</button>
      <button class="btn" type="button" onclick={() => void settle('rollback')}>{$tr('wn.revert')}</button>
    </div>
  {/if}

  {#if loading && !data}
    <p class="muted">{$tr('win.loading')}</p>
  {:else if error}
    <p class="err" role="alert">{error}</p>
  {:else if data}
    <div class="ifaces">
      {#each data.adapters as a (a.index)}
        {@const open = opened.includes(a.index) || editing?.index === a.index}
        <article class="card iface" class:open>
          <button class="iface-head" type="button" aria-expanded={open} onclick={() => toggle(a.index)}>
            <span class="hname">
              <h3 class="mono">{a.name}</h3>
              <span class="badges">
                <span class="tag" class:up={a.up} class:down={!a.up}>{a.up ? 'UP' : 'DOWN'}</span>
                <span class="tag">{a.virtual ? $tr('wn.virtual') : $tr('wn.physical')}</span>
                <span class="tag">{a.dhcp ? $tr('wn.dhcp') : $tr('wn.static')}</span>
              </span>
            </span>
            {#if !open}<span class="sum mono">{a.ipv4[0] ?? '—'}</span>{/if}
            <span class="chev" class:up={open}><PixIcon name="chev" /></span>
          </button>

          {#if open}
            <dl class="facts">
              <div class="fact"><dt>{$tr('wn.desc')}</dt><dd>{a.description}</dd></div>
              <div class="fact"><dt>{$tr('wn.mac')}</dt><dd class="mono">{a.mac || '—'}</dd></div>
              <div class="fact"><dt>MTU</dt><dd>{a.mtu}</dd></div>
              <div class="fact"><dt>{$tr('wn.mode')}</dt><dd>{a.dhcp ? $tr('wn.dhcp') : $tr('wn.static')}</dd></div>
              <div class="fact"><dt>IPv4</dt><dd class="mono">{a.ipv4.join(', ') || '—'}</dd></div>
              <div class="fact"><dt>{$tr('wn.gateway')}</dt><dd class="mono">{a.gateway && a.gateway !== '0.0.0.0' ? a.gateway : '—'}</dd></div>
              <div class="fact"><dt>DNS</dt><dd class="mono">{a.dns.join(', ') || '—'}</dd></div>
              <div class="fact"><dt>{$tr('wn.speed')}</dt><dd>{a.speed_mbps ? `${a.speed_mbps} Mbit/s` : '—'}</dd></div>
            </dl>

            {#if editing?.index !== a.index}
              <div class="row">
                <button class="btn" type="button" disabled={busy || !!pending} onclick={() => edit(a)}>{$tr('wn.edit')}</button>
              </div>
            {:else}
              <form class="form" onsubmit={(e) => { e.preventDefault(); review(); }}>
                <p class="small warnText">{$tr('wn.warnCarrier')}</p>
                <label><span>{$tr('wn.mode')}</span>
                  <Select bind:value={fMode} options={modeOptions} />
                </label>
                {#if fMode === 'static'}
                  <label><span>{$tr('wn.ip')}</span><input bind:value={fIp} placeholder="192.168.1.20" spellcheck="false" /></label>
                  <label><span>{$tr('wn.prefix')}</span><input type="number" min="1" max="30" bind:value={fPrefix} /></label>
                  <label><span>{$tr('wn.gateway')}</span><input bind:value={fGw} placeholder="192.168.1.1" spellcheck="false" /></label>
                {/if}
                <label><span>{$tr('wn.dns')}</span><input bind:value={fDns} placeholder="1.1.1.1, 8.8.8.8" spellcheck="false" /></label>
                <div class="row">
                  <button class="btn primary" type="submit" disabled={busy}>{$tr('wn.apply')}</button>
                  <button class="btn" type="button" onclick={() => (editing = null)}>{$tr('win.cancel')}</button>
                </div>
              </form>
            {/if}
          {/if}
        </article>
      {/each}
    </div>
  {/if}
</div>

{#if asking}
  <WinConfirm text={$tr('wn.confirmApply')} danger onyes={() => void apply()} onclose={() => (asking = null)} />
{/if}

<style>
  .wrap { display: flex; flex-direction: column; gap: 12px; }
  .bar { display: flex; align-items: center; justify-content: space-between; gap: 9px; }
  .bar h3 { margin: 0; }
  .ifaces { display: flex; flex-direction: column; gap: 12px; }
  .iface { display: flex; flex-direction: column; gap: 12px; min-width: 0; }
  .iface-head { display: flex; align-items: center; gap: 6px; width: 100%; min-height: 32px; padding: 0; background: none; color: var(--ink); border: 0; font: inherit; text-align: left; cursor: pointer; }
  .hname { display: flex; flex-direction: column; align-items: flex-start; gap: 4px; min-width: 0; }
  .hname h3 { margin: 0; }
  .badges { display: flex; flex-wrap: wrap; gap: 6px; }
  .tag { display: inline-block; padding: 0 5px; border: 2px solid var(--line); background: var(--surface); font-size: 12px; line-height: 1.5; }
  .tag.up { color: var(--good); border-color: var(--good); }
  .tag.down { color: var(--muted); }
  .sum { flex: 1; text-align: right; margin-left: 6px; overflow-wrap: anywhere; }
  .chev { display: grid; place-items: center; flex: none; width: 25px; height: 28px; margin-left: auto; }
  .sum + .chev { margin-left: 0; }
  .chev.up { transform: rotate(180deg); }
  .facts { margin: 0; display: flex; flex-direction: column; gap: 4px; }
  .fact { display: flex; justify-content: space-between; align-items: baseline; gap: 11px; min-width: 0; }
  .fact dt { color: var(--muted); flex: none; }
  .fact dd { margin: 0; text-align: right; overflow-wrap: anywhere; }
  @media (min-width: 721px) {
    .iface.open .facts { display: grid; grid-template-columns: repeat(auto-fill, minmax(min(234px, 100%), 1fr)); gap: 4px 28px; }
    .form { max-width: 640px; }
  }
  .row { display: flex; gap: 7px; flex-wrap: wrap; }
  .warn { padding: 8px 10px; border: 2px solid var(--warn); color: var(--warn); display: flex; align-items: center; gap: 9px; flex-wrap: wrap; }
  .warnText { color: var(--warn); }
  .err { color: var(--crit); margin: 0; }
  .form { display: flex; flex-direction: column; gap: 10px; }
  .form label { display: flex; flex-direction: column; gap: 4px; font-size: 12px; }
  .form :global(.sel) { padding: 8px 10px; background: var(--bg); }
  .form input { padding: 8px 10px; font: inherit; color: var(--ink); background: var(--bg); border: 2px solid var(--line); }
  p { margin: 0; }
</style>
