<script lang="ts">
  import { untrack } from 'svelte';
  import { api, ApiError, type F2bBan, type F2bEvent } from '../../../api';
  import { locale, t, tr as T } from '../../../i18n';
  import { showToast } from '../../toast';
  import Modal from '../../Modal.svelte';
  import { formatDateTime } from '../format';

  let {
    serverId,
    bans,
    onchanged,
    onclose,
  }: {
    serverId: string;
    bans: F2bBan[];
    onchanged: () => Promise<void>;
    onclose: () => void;
  } = $props();

  const tx = (key: string, vars?: Record<string, string | number>): string => t(key, undefined, vars);

  let tab = $state<'now' | 'hist'>('now');
  let search = $state('');
  let selected = $state<string[]>([]);
  let confirmAll = $state(false);
  let busy = $state(false);
  let error = $state<string | null>(null);

  let events = $state<F2bEvent[]>([]);
  let total = $state(0);
  let logPath = $state('');
  let histLoaded = $state(false);
  let histError = $state(false);

  const q = $derived(search.trim());
  const shownBans = $derived(q ? bans.filter((b) => b.ip.includes(q)) : bans);
  const shownEvents = $derived(q ? events.filter((e) => e.ip.includes(q)) : events);
  const bannedSet = $derived(new Set(bans.map((b) => b.ip)));
  const allSelected = $derived(shownBans.length > 0 && shownBans.every((b) => selected.includes(b.ip)));

  async function loadHistory(): Promise<void> {
    try {
      const h = await api.securityBans(serverId, 200);
      events = h.events;
      total = h.total;
      logPath = h.log;
      histError = !h.available;
    } catch {
      histError = true;
    } finally {
      histLoaded = true;
    }
  }

  $effect(() => {
    if (tab === 'hist' && !histLoaded) untrack(() => void loadHistory());
  });

  function toggle(ip: string): void {
    selected = selected.includes(ip) ? selected.filter((x) => x !== ip) : [...selected, ip];
  }

  function toggleAll(): void {
    selected = allSelected ? [] : shownBans.map((b) => b.ip);
  }

  function errorOf(e: unknown): string {
    return e instanceof ApiError && e.serverMessage ? e.serverMessage : tx('common.genericError');
  }

  async function unban(target: { ips: string[] } | { all: true }, doneKey: string, n: number): Promise<void> {
    if (busy) return;
    busy = true;
    error = null;
    try {
      await api.securityUnban(serverId, target);
      showToast(tx(doneKey, { n }));
      selected = [];
      confirmAll = false;
      histLoaded = false;
      await onchanged();
    } catch (e) {
      error = errorOf(e);
    } finally {
      busy = false;
    }
  }

  async function banAgain(ip: string): Promise<void> {
    if (busy) return;
    busy = true;
    error = null;
    try {
      await api.securityBan(serverId, ip, 0);
      showToast(tx('sec.f2b.banned.done', { ip }));
      histLoaded = false;
      await onchanged();
      if (tab === 'hist') await loadHistory();
    } catch (e) {
      error = errorOf(e);
    } finally {
      busy = false;
    }
  }

  /** When the ban of the address ends, as a relative text. */
  function until(b: F2bBan): string {
    if (!b.unban_at) return $T('sec.f2b.bans.never');
    const left = b.unban_at - Math.floor(Date.now() / 1000);
    if (left <= 0) return $T('sec.f2b.bans.soon');
    if (left < 3600) return $T('sec.f2b.bans.inMin', { n: Math.max(1, Math.round(left / 60)) });
    if (left < 86400) return $T('sec.f2b.bans.inH', { n: Math.round(left / 3600) });
    return $T('sec.f2b.bans.inD', { n: Math.round(left / 86400) });
  }

  function eventState(e: F2bEvent): string {
    if (e.event === 'ban') return bannedSet.has(e.ip) ? 'sec.f2b.hist.active' : 'sec.f2b.hist.over';
    return '';
  }
</script>

<Modal title={$T('sec.f2b.bansTitle')} labelId="f2b-bans-title" wide {onclose}>
  <div class="bar">
    <div class="row" role="group" aria-label={$T('sec.f2b.bansTitle')}>
      <button class="chip" type="button" aria-pressed={tab === 'now'} onclick={() => (tab = 'now')}>{$T('sec.f2b.bans.now')} <span class="muted cnt">{bans.length}</span></button>
      <button class="chip" type="button" aria-pressed={tab === 'hist'} onclick={() => (tab = 'hist')}>{$T('sec.f2b.bans.hist')} {#if histLoaded}<span class="muted cnt">{total}</span>{/if}</button>
    </div>
    <input class="input mono" type="search" placeholder={$T('sec.f2b.bans.search')} aria-label={$T('sec.f2b.bans.search')} bind:value={search} />
  </div>

  {#if tab === 'now'}
    {#if shownBans.length === 0}
      <p class="muted">{bans.length === 0 ? $T('sec.f2b.bans.none') : $T('sec.f2b.bans.nothing')}</p>
    {:else}
      <div class="tw">
        <table class="tbl">
          <thead>
            <tr>
              <th class="chk"><input type="checkbox" aria-label={$T('sec.f2b.bans.all')} checked={allSelected} onchange={toggleAll} /></th>
              <th>{$T('sec.col.ip')}</th>
              <th>{$T('sec.f2b.bans.jail')}</th>
              <th>{$T('sec.f2b.bans.since')}</th>
              <th>{$T('sec.f2b.bans.until')}</th>
              <th class="num">{$T('sec.col.attempts')}</th>
              <th class="num"><span class="sr">{$T('sec.f2b.bans.actions')}</span></th>
            </tr>
          </thead>
          <tbody>
            {#each shownBans as b (b.jail + b.ip)}
              <tr>
                <td class="chk"><input type="checkbox" aria-label={b.ip} checked={selected.includes(b.ip)} onchange={() => toggle(b.ip)} /></td>
                <td class="mono">{b.ip}</td>
                <td>{b.jail}</td>
                <td class="small muted">{b.banned_at ? formatDateTime(b.banned_at, $locale) : '—'}</td>
                <td class="small">{until(b)}</td>
                <td class="num">{b.attempts ? b.attempts.toLocaleString($locale) : '—'}</td>
                <td class="num"><button class="btn sm" type="button" disabled={busy} onclick={() => void unban({ ips: [b.ip] }, 'sec.f2b.unbanned', 1)}>{$T('sec.f2b.unban')}</button></td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
      <div class="foot">
        <span class="small muted">{$T('sec.f2b.bans.selected', { n: selected.length })}</span>
        <div class="row">
          <button class="btn sm" type="button" disabled={busy || selected.length === 0} onclick={() => void unban({ ips: selected }, 'sec.f2b.unbannedN', selected.length)}>{$T('sec.f2b.unbanSelected')}</button>
          <button class="btn sm danger" type="button" disabled={busy || bans.length === 0} onclick={() => (confirmAll = true)}>{$T('sec.f2b.unbanAll')}</button>
        </div>
      </div>
      {#if confirmAll}
        <div class="confirm stack" role="alertdialog" aria-label={$T('sec.f2b.unbanAll')}>
          <p>{$T('sec.f2b.unbanAllText', { n: bans.length })}</p>
          <div class="row end">
            <button class="btn" type="button" disabled={busy} onclick={() => (confirmAll = false)}>{$T('common.cancel')}</button>
            <button class="btn danger" type="button" disabled={busy} onclick={() => void unban({ all: true }, 'sec.f2b.unbannedN', bans.length)}>{$T('sec.f2b.unbanAll')}</button>
          </div>
        </div>
      {/if}
    {/if}
  {:else if !histLoaded}
    <p class="muted">{$T('common.loading')}</p>
  {:else if histError}
    <p class="muted">{$T('sec.f2b.hist.error', { path: logPath || '/var/log/fail2ban.log' })}</p>
  {:else if shownEvents.length === 0}
    <p class="muted">{$T('sec.f2b.hist.none')}</p>
  {:else}
    <div class="tw">
      <table class="tbl">
        <thead>
          <tr>
            <th>{$T('sec.col.time')}</th>
            <th>{$T('sec.f2b.hist.event')}</th>
            <th>{$T('sec.col.ip')}</th>
            <th>{$T('sec.f2b.bans.jail')}</th>
            <th class="num"><span class="sr">{$T('sec.f2b.bans.actions')}</span></th>
          </tr>
        </thead>
        <tbody>
          {#each shownEvents as e, i (i)}
            <tr>
              <td class="small muted">{formatDateTime(e.time, $locale)}</td>
              <td><span class="badge {e.event === 'ban' ? 'crit' : 'ok'}">{$T(e.event === 'ban' ? 'sec.f2b.hist.ban' : 'sec.f2b.hist.unban')}</span></td>
              <td class="mono">{e.ip}</td>
              <td>{e.jail}</td>
              <td class="num">
                {#if e.event === 'ban'}
                  <span class="small muted">{$T(eventState(e))}</span>
                {:else if !bannedSet.has(e.ip)}
                  <button class="btn sm" type="button" disabled={busy} onclick={() => void banAgain(e.ip)}>{$T('sec.f2b.hist.again')}</button>
                {/if}
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
    <p class="small muted">{$T('sec.f2b.hist.shown', { n: shownEvents.length, total, path: logPath })}</p>
  {/if}

  {#if error}<p class="error" role="alert">{error}</p>{/if}
  <div class="row end">
    <span class="small muted grow">{$T('sec.f2b.bans.audit')}</span>
    <button class="btn" type="button" onclick={onclose}>{$T('common.close')}</button>
  </div>
</Modal>

<style>
  p {
    margin: 0;
  }

  .bar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
  }

  .bar .input {
    width: 220px;
    height: 36px;
    padding-block: 0;
  }

  .row :global(.chip) {
    display: inline-flex;
    align-items: center;
    height: 36px;
    padding-block: 0;
  }

  .cnt {
    margin-left: 6px;
  }

  .tw {
    overflow-x: auto;
    max-height: 340px;
    overflow-y: auto;
  }

  .tbl {
    width: 100%;
    border-collapse: collapse;
    font-size: 13px;
  }

  .tbl th {
    font-family: var(--font-display);
    font-size: 12px;
    font-weight: 400;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: var(--muted);
    text-align: left;
    padding: 6px 8px;
    border-bottom: 2px solid var(--line);
    white-space: nowrap;
  }

  .tbl td {
    padding: 7px 8px;
    border-bottom: 2px dashed var(--seg-off);
    vertical-align: middle;
  }

  .num {
    text-align: right;
    white-space: nowrap;
  }

  .chk {
    width: 34px;
  }

  .tbl :global(.btn.sm),
  .foot :global(.btn.sm) {
    display: inline-flex;
    align-items: center;
    height: 30px;
    padding-block: 0;
    font-size: 13px;
  }

  .foot {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
  }

  .confirm {
    padding: 12px;
    border: 2px solid var(--crit);
  }

  .badge {
    display: inline-block;
    padding: 1px 6px;
    border: 2px solid currentColor;
    font-size: 12px;
    white-space: nowrap;
  }

  .badge.crit {
    color: var(--crit);
  }

  .badge.ok {
    color: var(--good);
  }

  .error {
    color: var(--crit);
  }

  .grow {
    flex: 1;
  }

  .sr {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip: rect(0 0 0 0);
  }
</style>
