<script lang="ts">
  import { untrack } from 'svelte';
  import { api, ApiError, type HostUser, type HostUsersState, type ServerDetail } from '../../../api';
  import { locale, t, tr as T } from '../../../i18n';
  import { openTermWindow } from '../../terminal/windows';
  import Modal from '../../Modal.svelte';
  import ActionMenu from '../../ui/ActionMenu.svelte';
  import PixIcon from '../../ui/PixIcon.svelte';
  import { isPhone } from '../../ui/viewport';
  import { showToast } from '../../toast';
  import { formatDateTime } from '../format';
  import UserAddModal from './UserAddModal.svelte';
  import UserConfirmModal from './UserConfirmModal.svelte';
  import UserDeleteModal from './UserDeleteModal.svelte';
  import UserModal from './UserModal.svelte';
  import UserPasswordModal from './UserPasswordModal.svelte';
  import { errorText, unavailableKind } from './svc';
  import { filterUsers, HOT_GROUPS, lastAdmin, sudoTone } from './users-util';
  import './phone-c.css';

  let { server, canTerminal = false, canWrite = true }: { server: ServerDetail; canTerminal?: boolean; canWrite?: boolean } = $props();

  type LoadState = 'loading' | 'ready' | 'offline' | 'disabled' | 'error';
  const POLL_MS = 30_000;

  let data = $state<HostUsersState | null>(null);
  let loadState = $state<LoadState>('loading');
  let seq = 0;

  async function load(id: string): Promise<void> {
    const req = ++seq;
    try {
      const res = await api.hostUsers(id);
      if (req !== seq) return;
      data = res;
      loadState = 'ready';
    } catch (e) {
      if (req !== seq) return;
      loadState = unavailableKind(e) ?? 'error';
    }
  }

  $effect(() => {
    const id = server.id;
    untrack(() => {
      data = null;
      loadState = 'loading';
      void load(id);
    });
    const timer = setInterval(() => void load(id), POLL_MS);
    return () => clearInterval(timer);
  });

  // ---- filters ----
  let fHuman = $state(false); // no kind pressed: everyone is shown
  let fSystem = $state(false);
  let fSudo = $state(false);
  let fOnline = $state(false);
  let q = $state('');
  const rows = $derived(data ? filterUsers(data.users, { human: fHuman, system: fSystem, sudo: fSudo, online: fOnline, q }) : []);

  const sums = $derived.by(() => {
    const us = data?.users ?? [];
    return {
      human: us.filter((u) => u.human).length,
      sudo: us.filter((u) => u.sudo !== 'none').length,
      sessions: us.reduce((a, u) => a + u.sessions, 0),
      locked: us.filter((u) => u.human && u.locked).length,
    };
  });

  // ---- dialogs ----
  let editing = $state<HostUser | null>(null);
  let adding = $state(false);
  let deleting = $state<HostUser | null>(null);
  let password = $state<HostUser | null>(null);
  /** Phone: the user whose action sheet is open. */
  let actionUser = $state<HostUser | null>(null);
  let confirm = $state<{ kind: 'lock' | 'unlock' | 'kill'; user: HostUser } | null>(null);

  const tx = (k: string, v?: Record<string, string | number>) => t(k, undefined, v);

  async function runConfirmed(): Promise<string | null> {
    const c = confirm;
    if (!c || !data) return null;
    try {
      if (c.kind === 'kill') await api.hostUserKill(server.id, c.user.name);
      else await api.hostUserUpdate(server.id, c.user.name, { locked: c.kind === 'lock' });
      showToast(tx(`usr.confirm.${c.kind}.done`, { user: c.user.name }));
      void load(server.id);
      return null;
    } catch (e) {
      return e instanceof ApiError && e.serverMessage ? e.serverMessage : errorText(e, tx);
    }
  }

  function menu(u: HostUser) {
    const system = !u.human;
    const only = data ? lastAdmin(data, u.name) && u.sudo !== 'none' : false;
    return [
      { key: 'editing', label: tx('usr.menu.props'), onselect: () => (editing = u) },
      { key: 'lock', label: u.locked ? tx('usr.menu.unlock') : tx('usr.menu.lock'), disabled: system || !canWrite || (!u.locked && only), onselect: () => (confirm = { kind: u.locked ? 'unlock' : 'lock', user: u }) },
      { key: 'pw', label: tx('usr.menu.password'), disabled: system || !canWrite, onselect: () => (password = u) },
      { key: 'kill', label: tx('usr.menu.kill'), disabled: system || !canWrite || u.sessions === 0, onselect: () => (confirm = { kind: 'kill', user: u }) },
      { key: 'del', label: tx('usr.menu.delete'), disabled: !u.human || u.protected || !canWrite, onselect: () => (deleting = u) },
    ];
  }

  function terminal(u: HostUser): void {
    openTermWindow(server.id, server.name, u.name);
  }

  const sudoLabel = (s: HostUser['sudo']) => $T(`usr.sudo.badge.${s}`);
  const unavailable = $derived(loadState === 'offline' || loadState === 'disabled');
</script>

<div class="stack">
  {#if loadState === 'loading'}
    <p class="muted">{$T('common.loading')}</p>
  {:else if loadState === 'offline'}
    <p class="notice" role="status">{$T('svc.offline')}</p>
  {:else if loadState === 'disabled'}
    <p class="notice" role="status">{$T('svc.disabled')}</p>
  {:else if loadState === 'error'}
    <p class="error" role="alert">{$T('usr.loadFailed')}</p>
  {/if}

  {#if data}
    <div class="sums">
      <div class="sum"><b>{sums.human}</b><span class="muted small">{$T('usr.sum.human')}</span></div>
      <div class="sum"><b>{sums.sudo}</b><span class="muted small">{$T('usr.sum.sudo')}</span></div>
      <div class="sum"><b>{sums.sessions}</b><span class="muted small">{$T('usr.sum.sessions')}</span></div>
      <div class="sum"><b>{sums.locked}</b><span class="muted small">{$T('usr.sum.locked')}</span></div>
    </div>

    {#if $isPhone}
      <div class="tools ptools">
        <input class="input search" type="search" bind:value={q} placeholder={$T('usr.search')} aria-label={$T('usr.search')} />
        <div class="pc-seg sm" role="group" aria-label={$T('usr.search')}>
          <button type="button" aria-pressed={fHuman} onclick={() => (fHuman = !fHuman)}>{$T('usr.f.human')}</button>
          <button type="button" aria-pressed={fSystem} onclick={() => (fSystem = !fSystem)}>{$T('usr.f.system')}</button>
          <button type="button" aria-pressed={fSudo} onclick={() => (fSudo = !fSudo)}>{$T('usr.f.sudo')}</button>
          <button type="button" aria-pressed={fOnline} onclick={() => (fOnline = !fOnline)}>{$T('usr.f.online')}</button>
        </div>
        <button class="btn primary add" type="button" disabled={unavailable || !canWrite} onclick={() => (adding = true)}>{$T('usr.add')}</button>
      </div>

      {#if rows.length === 0}
        <p class="muted empty">{$T('usr.empty')}</p>
      {:else}
        <div class="pc-list">
          {#each rows as u (u.name)}
            <button class="pc-li" type="button" onclick={() => (actionUser = u)}>
              <span class="pc-mn">
                <span class="pc-l1">
                  <b>{u.name}</b>
                  {#if u.sudo !== 'none'}<span class="pc-tag {sudoTone(u.sudo) === 'crit' ? 'crit' : 'warn'}">{sudoLabel(u.sudo)}</span>{/if}
                  {#if u.locked && u.human}<span class="pc-tag crit">{$T('usr.badge.locked')}</span>{/if}
                  {#if u.protected}<span class="pc-tag muted">{$T('usr.badge.protected')}</span>{/if}
                  {#if u.sessions > 0}<span class="pc-tag ok">{$T('usr.sessionsN', { n: u.sessions })}</span>{/if}
                </span>
                <span class="pc-l2 mono">{[u.uid, u.shell, u.human ? u.home : ''].filter((x) => x !== '').join(' · ')}</span>
                {#if u.gecos}<span class="pc-l2">{u.gecos}</span>{/if}
              </span>
              <PixIcon name="more" />
            </button>
          {/each}
        </div>
      {/if}
    {:else}
    <div class="tools">
      <input class="input search" type="search" bind:value={q} placeholder={$T('usr.search')} aria-label={$T('usr.search')} />
      <button class="chip" type="button" aria-pressed={fHuman} onclick={() => (fHuman = !fHuman)}>{$T('usr.f.human')}</button>
      <button class="chip" type="button" aria-pressed={fSystem} onclick={() => (fSystem = !fSystem)}>{$T('usr.f.system')}</button>
      <button class="chip" type="button" aria-pressed={fSudo} onclick={() => (fSudo = !fSudo)}>{$T('usr.f.sudo')}</button>
      <button class="chip" type="button" aria-pressed={fOnline} onclick={() => (fOnline = !fOnline)}>{$T('usr.f.online')}</button>
      <button class="btn primary add" type="button" disabled={unavailable || !canWrite} onclick={() => (adding = true)}>{$T('usr.add')}</button>
    </div>

    <div class="table-wrap">
      <table class="tbl">
        <thead>
          <tr>
            <th scope="col">{$T('usr.col.user')}</th>
            <th scope="col" class="num">UID</th>
            <th scope="col">{$T('usr.col.rights')}</th>
            <th scope="col">{$T('usr.col.groups')}</th>
            <th scope="col">{$T('usr.col.shell')}</th>
            <th scope="col">{$T('usr.col.last')}</th>
            <th scope="col">{$T('usr.col.now')}</th>
            <th scope="col"><span class="sr">{$T('usr.col.actions')}</span></th>
          </tr>
        </thead>
        <tbody>
          {#each rows as u (u.name)}
            <tr class="u" onclick={(e) => { if (!(e.target as HTMLElement).closest('.acts')) editing = u; }}>
              <td class="nw">
                <div class="uname">
                  <button class="link" type="button" onclick={() => (editing = u)}>{u.name}</button>
                  {#if u.locked && u.human}<span class="badge tone-warn">{$T('usr.badge.locked')}</span>{/if}
                  {#if u.protected}<span class="badge tone-muted">{$T('usr.badge.protected')}</span>{/if}
                </div>
                <div class="muted small">{u.gecos || (u.human ? '' : $T('usr.system'))}</div>
              </td>
              <td class="num">{u.uid}</td>
              <td>
                {#if u.sudo === 'none'}<span class="muted">—</span>{:else}<span class="badge tone-{sudoTone(u.sudo)}">{sudoLabel(u.sudo)}</span>{/if}
              </td>
              <td>
                <div class="gl">
                  {#each u.groups as g (g)}<span class="gtag" class:hot={HOT_GROUPS.includes(g)}>{g}</span>{:else}<span class="muted">—</span>{/each}
                </div>
              </td>
              <td class:muted={u.no_shell} class="mono small nw" title={u.shell}>{u.shell.split('/').pop()}</td>
              <td class="nw">
                {#if u.last_login > 0}
                  {formatDateTime(u.last_login, $locale)}
                  {#if u.last_from}<div class="muted small">{$T('usr.from', { ip: u.last_from })}</div>{/if}
                {:else}<span class="muted">{$T('usr.never')}</span>{/if}
              </td>
              <td class="nw">
                {#if u.sessions > 0}<span class="tone-ok"><span class="dot"></span>{$T('usr.sessionsN', { n: u.sessions })}</span>{:else}<span class="muted">—</span>{/if}
              </td>
              <td class="acts">
                <div class="act-row">
                  {#if canTerminal}
                    <button class="btn sm" type="button" disabled={u.no_shell || !server.online} title={u.no_shell ? $T('usr.noShell') : ''} onclick={() => terminal(u)}>{$T('usr.terminal')}</button>
                  {/if}
                  <ActionMenu label={$T('usr.menu')} items={menu(u)} />
                </div>
              </td>
            </tr>
          {:else}
            <tr><td colspan="8" class="muted empty">{$T('usr.empty')}</td></tr>
          {/each}
        </tbody>
      </table>
    </div>
    {/if}
    <p class="muted small">{$T('usr.legend', { shown: rows.length, total: data.users.length })}</p>
  {/if}
</div>

{#if actionUser && $isPhone}
  {@const au = actionUser}
  <Modal flush title={au.name} labelId="usr-act" onclose={() => (actionUser = null)}>
    {#if canTerminal}
      <button
        class="pc-act"
        type="button"
        disabled={au.no_shell || !server.online}
        onclick={() => {
          // `au` follows `actionUser`, so the user is taken before the sheet is closed.
          const target = au;
          actionUser = null;
          terminal(target);
        }}>{$T('mobC.usr.termAs', { user: au.name })}</button
      >
    {/if}
    {#each menu(au) as it (it.key)}
      <button
        class="pc-act"
        class:dng={it.key === 'del' || it.key === 'kill'}
        type="button"
        disabled={it.disabled}
        onclick={() => {
          actionUser = null;
          it.onselect();
        }}>{it.label}</button
      >
    {/each}
  </Modal>
{/if}

{#if editing && data}
  <UserModal
    serverId={server.id}
    user={editing}
    info={data}
    onclose={() => (editing = null)}
    ondone={() => { editing = null; void load(server.id); }}
    ondelete={(u) => { editing = null; deleting = u; }}
  />
{/if}
{#if adding && data}
  <UserAddModal serverId={server.id} info={data} onclose={() => (adding = false)} ondone={() => { adding = false; void load(server.id); }} />
{/if}
{#if deleting}
  <UserDeleteModal serverId={server.id} user={deleting} onclose={() => (deleting = null)} ondone={() => { deleting = null; void load(server.id); }} />
{/if}
{#if password}
  <UserPasswordModal serverId={server.id} user={password} onclose={() => (password = null)} />
{/if}
{#if confirm}
  <UserConfirmModal
    title={$T(`usr.confirm.${confirm.kind}.title`, { user: confirm.user.name })}
    text={$T(`usr.confirm.${confirm.kind}.text`, { user: confirm.user.name, n: confirm.user.sessions })}
    hint={$T(`usr.confirm.${confirm.kind}.hint`)}
    confirmLabel={$T(`usr.confirm.${confirm.kind}.do`)}
    danger={confirm.kind !== 'unlock'}
    onclose={() => (confirm = null)}
    onconfirm={runConfirmed}
  />
{/if}

<style>
  .sums {
    display: grid;
    grid-template-columns: repeat(4, minmax(0, 1fr));
    gap: 11px;
  }

  .sum {
    padding: 9px 11px;
    border: 2px solid var(--line);
  }

  .sum b {
    display: block;
    font-family: var(--font-display);
    font-weight: 400;
    font-size: 23px;
    line-height: 1.1;
  }

  .tools {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 7px;
  }

  .search {
    flex: 1 1 200px;
    max-width: 288px;
    height: 32px;
    padding-block: 0;
  }

  .add {
    margin-left: auto;
  }

  .table-wrap {
    overflow-x: auto;
  }

  .tbl {
    width: 100%;
    min-width: 810px;
    border-collapse: collapse;
    font-size: 12px;
  }

  .tbl th {
    padding: 5px 7px;
    text-align: left;
    font-family: var(--font-display);
    font-weight: 400;
    font-size: 11px;
    color: var(--muted);
    border-bottom: 2px solid var(--line);
    white-space: nowrap;
  }

  .tbl td {
    padding: 7px;
    vertical-align: middle;
    border-bottom: 1px dashed var(--seg-off);
  }

  .tbl tr.u {
    cursor: pointer;
  }

  .tbl tr.u:hover td {
    background: var(--seg-off);
  }

  .num {
    text-align: right;
  }

  .nw {
    white-space: nowrap;
  }

  .uname {
    display: flex;
    align-items: center;
    gap: 7px;
    font-weight: 600;
    white-space: nowrap;
  }

  .link {
    padding: 0;
    background: none;
    border: 0;
    color: inherit;
    cursor: pointer;
    font: inherit;
    font-weight: 600;
    text-align: left;
  }

  .gl {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
  }

  .gtag {
    padding: 0 5px;
    white-space: nowrap;
    border: 1px solid var(--muted);
    color: var(--muted);
    font-size: 11px;
  }

  .gtag.hot {
    border-color: var(--warn);
    color: var(--warn);
  }

  .badge {
    display: inline-block;
    padding: 0 5px;
    border: 2px solid currentColor;
    font-size: 11px;
    font-weight: 400;
    line-height: 1.35;
    white-space: nowrap;
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

  .tone-muted {
    color: var(--muted);
  }

  .dot {
    display: inline-block;
    width: 7px;
    height: 7px;
    margin-right: 5px;
    background: currentColor;
  }

  .acts {
    width: 1%;
    white-space: nowrap;
  }

  .act-row {
    display: flex;
    justify-content: flex-end;
    gap: 5px;
  }

  .empty {
    padding: 22px;
    text-align: center;
  }

  .notice {
    margin: 0;
    padding: 7px 11px;
    border: 2px solid var(--warn);
    color: var(--warn);
  }

  .sr {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip: rect(0 0 0 0);
  }

  @media (max-width: 860px) {
    .sums {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
  }

  @media (max-width: 720px) {
    .sums {
      grid-template-columns: repeat(2, minmax(0, 1fr));
      gap: 7px;
    }

    .sum {
      padding: 7px 9px;
      background: var(--surface);
      box-shadow: 3px 3px 0 var(--shade);
    }

    .sum b {
      font-size: 20px;
    }

    .ptools {
      flex-direction: column;
      align-items: stretch;
      gap: 9px;
    }

    .ptools .search {
      flex: none;
      max-width: none;
      width: 100%;
      height: auto;
    }

    .ptools .add {
      margin-left: 0;
      width: 100%;
    }

    .pc-l1 b {
      font-family: var(--font-display);
      font-size: 15px;
      font-weight: 400;
    }

    .pc-l2 {
      display: block;
    }
  }
</style>
