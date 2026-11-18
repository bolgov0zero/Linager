<script lang="ts">
  import { ApiError, openStream, winCall, type ServerDetail, type WinUpdate } from '../../../api';
  import { tr } from '../../../i18n';
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
      error = e instanceof ApiError && e.serverMessage ? e.serverMessage : $tr('win.error');
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
      showToast($tr('wup.done'));
      void check();
    };
  }

  $effect(() => () => socket?.close());

  const size = (b: number) => (b > 1 << 20 ? `${(b / (1 << 20)).toFixed(1)} MB` : `${Math.max(1, Math.round(b / 1024))} KB`);
  const toggle = (id: string) => (picked = picked.includes(id) ? picked.filter((x) => x !== id) : [...picked, id]);
</script>

<div class="wrap">
  <div class="bar">
    <h3>{$tr('wup.title')}</h3>
    <div class="row">
      <button class="btn" type="button" disabled={searching || running} onclick={() => void check()}>{$tr('wup.check')}</button>
      {#if list && list.length > 0}
        <button class="btn" type="button" disabled={running || picked.length === 0} onclick={() => (confirming = [...picked])}>{$tr('wup.installSel')}</button>
        <button class="btn primary" type="button" disabled={running} onclick={() => (confirming = [])}>{$tr('wup.installAll')}</button>
      {/if}
    </div>
  </div>

  {#if rebootPending || server.sample?.reboot_required}
    <p class="warn">{$tr('wup.rebootPending')} <RebootButton {server} /></p>
  {/if}
  {#if searching}
    <p class="muted">{$tr('wup.checking')}</p>
  {:else if error}
    <p class="err" role="alert">{error}</p>
  {:else if list === null}
    <p class="muted">{$tr('wup.first')}</p>
  {:else if list.length === 0}
    <p class="muted">{$tr('wup.none')}</p>
  {:else}
    {#each list as u (u.id)}
      <label class="card urow">
        <input type="checkbox" checked={picked.includes(u.id)} onchange={() => toggle(u.id)} disabled={running} />
        <span class="t">
          <b>{u.title}</b>
          <span class="small muted">{u.kb} · {size(u.size)}{u.severity ? ` · ${u.severity}` : ''}{u.need_reboot ? ` · ${$tr('wup.rebootNote')}` : ''}</span>
        </span>
        {#if u.security}<span class="chip sec">{$tr('wup.security')}</span>{/if}
      </label>
    {/each}
  {/if}

  {#if log || running}
    <h3>{$tr('wup.log')} {running ? `· ${$tr('wup.running')}` : ''}</h3>
    <pre class="log">{log}</pre>
  {/if}
</div>

{#if confirming}
  {@const ids = confirming}
  <WinConfirm text={$tr('wup.confirm', { n: ids.length || list?.length || 0 })} onyes={() => install(ids)} onclose={() => (confirming = null)} />
{/if}

<style>
  .wrap { display: flex; flex-direction: column; gap: 10px; }
  .bar { display: flex; align-items: center; justify-content: space-between; gap: 10px; flex-wrap: wrap; }
  h3 { margin: 6px 0; }
  .row { display: flex; gap: 8px; flex-wrap: wrap; }
  .urow { display: flex; align-items: center; gap: 10px; padding: 8px 12px; cursor: pointer; }
  .t { display: flex; flex-direction: column; flex: 1; min-width: 0; overflow-wrap: anywhere; }
  .chip { padding: 1px 6px; border: 2px solid var(--line); font-size: 12px; }
  .chip.sec { border-color: var(--warn); color: var(--warn); }
  .warn { margin: 0; padding: 8px 10px; border: 2px solid var(--warn); color: var(--warn); display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
  .err { color: var(--crit); margin: 0; }
  .log { margin: 0; padding: 10px; background: #0f1218; color: #e6e9ef; border: 2px solid var(--line); max-height: 320px; overflow: auto; font-size: 12px; white-space: pre-wrap; }
  p { margin: 0; }
</style>
