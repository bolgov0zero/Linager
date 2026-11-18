<script lang="ts">
  import { ApiError, openStream, winCall, type ServerDetail, type WinUpdate } from '../../../api';
  import { tr as T } from '../../../i18n';
  import { showToast } from '../../toast';
  import RebootButton from './RebootButton.svelte';
  import WinConfirm from './WinConfirm.svelte';

  let { server }: { server: ServerDetail } = $props();
  const serverId = $derived(server.id);

  let list = $state<WinUpdate[] | null>(null);
  let rebootPending = $state(false);
  let searching = $state(false);
  let error = $state('');
  let picked = $state<string[]>([]);
  let confirming = $state<string[] | null>(null);
  let log = $state('');
  let running = $state(false);
  let socket: WebSocket | null = null;

  async function check(): Promise<void> {
    searching = true;
    error = '';
    try {
      const r = await winCall<{ updates: WinUpdate[]; reboot_pending: boolean }>(serverId, 'winupdate', 'list');
      list = r.updates;
      rebootPending = r.reboot_pending;
      picked = [];
    } catch (e) {
      error = e instanceof ApiError && e.serverMessage ? e.serverMessage : $T('win.error');
    } finally {
      searching = false;
    }
  }

  function install(ids: string[]): void {
    confirming = null;
    log = '';
    running = true;
    const ws = openStream(serverId, 'winupdate', ids.length ? { ids: ids.join(',') } : {});
    socket = ws;
    const dec = new TextDecoder();
    ws.onmessage = (ev) => {
      if (typeof ev.data === 'string') return;
      log += dec.decode(ev.data as ArrayBuffer).replace(/\r\n/g, '\n');
    };
    ws.onclose = () => {
      running = false;
      socket = null;
      showToast($T('wup.done'));
      void check();
    };
  }

  $effect(() => () => socket?.close());

  const securityList = $derived((list ?? []).filter((u) => u.security));
  let onlySec = $state(false);
  const shown = $derived(onlySec ? securityList : (list ?? []));
  const size = (b: number) => (b > 1 << 20 ? `${(b / (1 << 20)).toFixed(1)} MB` : `${Math.max(1, Math.round(b / 1024))} KB`);
  const toggle = (id: string) => (picked = picked.includes(id) ? picked.filter((x) => x !== id) : [...picked, id]);
</script>

<div class="wrap">
  <div class="head">
    <div class="summary">
      {#if list}
        <p class="big">{list.length}</p>
        <p class="muted small">{$T('wup.summary', { n: list.length, s: securityList.length })}</p>
      {:else if searching}
        <p class="muted">{$T('wup.checking')}</p>
      {:else}
        <p class="muted">{$T('wup.first')}</p>
      {/if}
    </div>
    <div class="btns">
      <button class="btn" type="button" disabled={searching || running} onclick={() => void check()}>{$T('wup.check')}</button>
      <button class="btn" type="button" disabled={running || securityList.length === 0} onclick={() => (confirming = securityList.map((u) => u.id))}>{$T('wup.installSec')}</button>
      <button class="btn primary" type="button" disabled={running || !list || list.length === 0} onclick={() => (confirming = [])}>{$T('wup.installAll')}</button>
    </div>
  </div>

  {#if rebootPending || server.sample?.reboot_required}
    <p class="banner" role="status">{$T('wup.rebootPending')} <RebootButton {server} /></p>
  {/if}
  {#if error}
    <p class="err" role="alert">{error}</p>
  {/if}

  {#if list && list.length > 0}
    <div class="tools">
      <button class="chip" type="button" aria-pressed={onlySec} onclick={() => (onlySec = !onlySec)}>{$T('wup.f.security')}</button>
      <button class="btn sm" type="button" disabled={running || picked.length === 0} onclick={() => (confirming = [...picked])}>{$T('wup.installSel')}</button>
    </div>
    <div class="table-wrap">
      <table class="tbl">
        <thead><tr><th scope="col"></th><th scope="col">{$T('wup.colTitle')}</th><th scope="col">KB</th><th scope="col" class="num">{$T('wup.colSize')}</th><th scope="col">{$T('wup.colKind')}</th></tr></thead>
        <tbody>
          {#each shown as u (u.id)}
            <tr>
              <td><input type="checkbox" checked={picked.includes(u.id)} onchange={() => toggle(u.id)} disabled={running} aria-label={u.title} /></td>
              <td>{u.title}{#if u.need_reboot}<div class="muted small">{$T('wup.rebootNote')}</div>{/if}</td>
              <td class="mono nw">{u.kb}</td>
              <td class="num nw">{size(u.size)}</td>
              <td class="nw">{#if u.security}<span class="badge tone-warn">{$T('wup.security')}</span>{/if}{#if u.severity} <span class="muted small">{u.severity}</span>{/if}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {:else if list && list.length === 0}
    <p class="muted">{$T('wup.none')}</p>
  {/if}

  {#if log || running}
    <section class="card stack" aria-label={$T('wup.log')}>
      <div class="row between"><b>{$T('wup.log')}</b><span class="muted small">{running ? $T('wup.running') : $T('wup.done')}</span></div>
      <pre class="log">{log}</pre>
    </section>
  {/if}
</div>

{#if confirming}
  {@const ids = confirming}
  <WinConfirm text={$T('wup.confirm', { n: ids.length || list?.length || 0 })} onyes={() => install(ids)} onclose={() => (confirming = null)} />
{/if}

<style>
  .wrap { display: flex; flex-direction: column; gap: 12px; }
  .head { display: flex; align-items: center; justify-content: space-between; gap: 11px; flex-wrap: wrap; }
  .summary .big { margin: 0; font-family: var(--font-display); font-weight: 400; font-size: 31px; line-height: 1.1; }
  .btns, .tools { display: flex; gap: 7px; flex-wrap: wrap; align-items: center; }
  .banner { margin: 0; padding: 7px 9px; border: 2px solid var(--warn); color: var(--warn); display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
  .table-wrap { overflow-x: auto; }
  .tbl { width: 100%; min-width: 576px; border-collapse: collapse; font-size: 12px; }
  .tbl th { padding: 6px 8px; text-align: left; font-family: var(--font-display); font-weight: 400; font-size: 12px; color: var(--muted); border-bottom: 2px solid var(--line); white-space: nowrap; }
  .tbl td { padding: 8px; vertical-align: middle; border-bottom: 1px dashed var(--seg-off); overflow-wrap: anywhere; }
  .num { text-align: right; }
  .nw { white-space: nowrap; }
  .badge { display: inline-block; padding: 0 5px; border: 2px solid currentColor; font-size: 11px; line-height: 1.35; white-space: nowrap; }
  .tone-warn { color: var(--warn); }
  .stack { display: flex; flex-direction: column; gap: 8px; }
  .row { display: flex; gap: 7px; align-items: center; }
  .row.between { justify-content: space-between; }
  .err { color: var(--crit); margin: 0; }
  .log { margin: 0; padding: 9px; background: #0f1218; color: #e6e9ef; border: 2px solid var(--line); max-height: 288px; overflow: auto; font-size: 11px; white-space: pre-wrap; }
  p { margin: 0; }
</style>
