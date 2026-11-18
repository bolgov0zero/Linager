<script lang="ts">
  import { ApiError, winCall, type ServerDetail, type WinAdapter, type WinNetApply, type WinNetState } from '../../../api';
  import { tr } from '../../../i18n';
  import Modal from '../../Modal.svelte';
  import { showToast } from '../../toast';
  import WinConfirm from './WinConfirm.svelte';

  let { server }: { server: ServerDetail } = $props();
  const serverId = $derived(server.id);

  let data = $state<WinNetState | null>(null);
  let loading = $state(true);
  let error = $state('');
  let busy = $state(false);
  let editing = $state<WinAdapter | null>(null);
  let asking = $state<WinNetApply | null>(null);
  let pending = $state<{ id: string; deadline: number } | null>(null);
  let left = $state(0);

  let fMode = $state<'dhcp' | 'static'>('dhcp');
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
    {#each data.adapters as a (a.index)}
      <div class="card arow">
        <div class="who">
          <b>{a.name}</b> <span class="muted small">· {a.description}{a.virtual ? ` · ${$tr('wn.virtual')}` : ''}</span>
          <div class="small mono">
            {a.up ? $tr('wn.up') : $tr('wn.down')} · {a.dhcp ? $tr('wn.dhcp') : $tr('wn.static')} · {a.ipv4.join(', ') || '—'}
            {#if a.gateway && a.gateway !== '0.0.0.0'} · {$tr('wn.gateway')} {a.gateway}{/if}
          </div>
          <div class="small muted mono">DNS {a.dns.join(', ') || '—'} · {$tr('wn.mac')} {a.mac}{a.speed_mbps ? ` · ${a.speed_mbps} Mbit/s` : ''} · MTU {a.mtu}</div>
        </div>
        <button class="btn sm" type="button" disabled={busy || !!pending} onclick={() => edit(a)}>{$tr('wn.edit')}</button>
      </div>
    {/each}
  {/if}
</div>

{#if editing && !asking}
  <Modal title={editing.name} labelId="wn-edit-title" onclose={() => (editing = null)}>
    <form class="form" onsubmit={(e) => { e.preventDefault(); review(); }}>
      <p class="small warnText">{$tr('wn.warnCarrier')}</p>
      <label><span>{$tr('wn.mode')}</span>
        <select bind:value={fMode}><option value="dhcp">{$tr('wn.dhcp')}</option><option value="static">{$tr('wn.static')}</option></select>
      </label>
      {#if fMode === 'static'}
        <label><span>{$tr('wn.ip')}</span><input bind:value={fIp} placeholder="192.168.1.20" spellcheck="false" /></label>
        <label><span>{$tr('wn.prefix')}</span><input type="number" min="1" max="30" bind:value={fPrefix} /></label>
        <label><span>{$tr('wn.gateway')}</span><input bind:value={fGw} placeholder="192.168.1.1" spellcheck="false" /></label>
      {/if}
      <label><span>{$tr('wn.dns')}</span><input bind:value={fDns} placeholder="1.1.1.1, 8.8.8.8" spellcheck="false" /></label>
      <div class="row end">
        <button class="btn" type="button" onclick={() => (editing = null)}>{$tr('win.cancel')}</button>
        <button class="btn primary" type="submit">{$tr('wn.apply')}</button>
      </div>
    </form>
  </Modal>
{/if}
{#if asking}
  <WinConfirm text={$tr('wn.confirmApply')} danger onyes={() => void apply()} onclose={() => (asking = null)} />
{/if}

<style>
  .wrap { display: flex; flex-direction: column; gap: 10px; }
  .bar { display: flex; align-items: center; justify-content: space-between; gap: 10px; }
  h3 { margin: 6px 0; }
  .arow { display: flex; align-items: center; justify-content: space-between; gap: 10px; padding: 10px 12px; flex-wrap: wrap; }
  .who { min-width: 0; overflow-wrap: anywhere; }
  .warn { padding: 8px 10px; border: 2px solid var(--warn); color: var(--warn); display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
  .warnText { color: var(--warn); }
  .err { color: var(--crit); margin: 0; }
  .form { display: flex; flex-direction: column; gap: 10px; min-width: min(360px, 80vw); }
  .form label { display: flex; flex-direction: column; gap: 4px; font-size: 13px; }
  .form input, .form select { padding: 8px 10px; font: inherit; color: var(--ink); background: var(--bg); border: 2px solid var(--line); }
  .row.end { display: flex; justify-content: flex-end; gap: 8px; }
  p { margin: 0; }
</style>
