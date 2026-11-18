<script lang="ts">
  import { onMount } from 'svelte';
  import { api, ApiError, isUnauthorized, type Role, type UserAccess, type UserRow } from '../../api';
  import { locale, t, tr } from '../../i18n';
  import { showToast } from '../toast';
  import { formatDateTime } from '../server/format';
  import AccessEditor from './AccessEditor.svelte';
  import RowMenu from '../ui/RowMenu.svelte';
  import { isPhone } from '../ui/viewport';

  let { onauthlost }: { onauthlost: () => void } = $props();

  type Action = { kind: 'delete' | 'reset2fa' | 'password'; user: UserRow };

  let users = $state<UserRow[]>([]);
  let loaded = $state(false);
  let failed = $state(false);

  let adding = $state(false);
  let newLogin = $state('');
  let newPassword = $state('');
  let newRole = $state<Role>('admin');
  let newAccess = $state<UserAccess>({ servers: [], groups: [], modules: [] });

  /** Changing the role and the access of an existing user. */
  let editing = $state<{ user: UserRow; role: Role; access: UserAccess } | null>(null);
  let addError = $state<string | null>(null);

  let action = $state<Action | null>(null);
  let resetPw = $state('');
  let actionError = $state<string | null>(null);
  let busy = $state(false);

  async function load(): Promise<void> {
    try {
      users = (await api.users()).users;
      failed = false;
    } catch (e) {
      if (isUnauthorized(e)) onauthlost();
      else failed = true;
    } finally {
      loaded = true;
    }
  }
  onMount(() => void load());

  function errText(e: unknown, fallback: string): string {
    if (e instanceof ApiError) {
      if (e.status === 0) return t('common.networkError');
      if (e.status === 409) return e.serverMessage === 'this login is taken' ? t('users.loginTaken') : (e.serverMessage ?? fallback);
      if (e.status === 422) return e.serverMessage ?? fallback;
    }
    return fallback;
  }

  async function create(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    if (busy) return;
    busy = true;
    addError = null;
    try {
      await api.userCreate(newLogin.trim(), newPassword, newRole, newAccess);
      newLogin = newPassword = '';
      newRole = 'admin';
      newAccess = { servers: [], groups: [], modules: [] };
      adding = false;
      showToast(t('users.createdToast'));
      await load();
    } catch (e) {
      if (isUnauthorized(e)) onauthlost();
      else addError = errText(e, t('common.genericError'));
    } finally {
      busy = false;
    }
  }

  function startEdit(user: UserRow): void {
    action = null;
    editing = { user, role: (user.role as Role) ?? 'admin', access: user.access ? { servers: [...user.access.servers], groups: [...user.access.groups], modules: [...user.access.modules] } : { servers: [], groups: [], modules: [] } };
    actionError = null;
  }

  async function saveEdit(): Promise<void> {
    const e = editing;
    if (!e || busy) return;
    busy = true;
    actionError = null;
    try {
      await api.userAccess(e.user.id, e.role, e.access);
      showToast(t('users.accessSaved'));
      editing = null;
      await load();
    } catch (err) {
      if (isUnauthorized(err)) onauthlost();
      else actionError = errText(err, t('common.genericError'));
    } finally {
      busy = false;
    }
  }

  function summary(u: UserRow): string {
    if (!u.access) return '';
    const n = u.access.servers.length + u.access.groups.length;
    return u.role === 'operator'
      ? t('users.accessSummaryOp', undefined, { n, m: u.access.modules.length })
      : t('users.accessSummaryAud', undefined, { n });
  }

  /** The row actions of a user in the sheet on phones. */
  function menu(u: UserRow) {
    return [
      { key: 'access', label: $tr('users.access'), onselect: () => startEdit(u) },
      { key: 'password', label: $tr('users.reset'), onselect: () => ask('password', u) },
      { key: 'reset2fa', label: $tr('users.reset2fa'), disabled: !u.totp_enabled && u.passkeys === 0, onselect: () => ask('reset2fa', u) },
      { key: 'delete', label: $tr('users.delete'), onselect: () => ask('delete', u) },
    ];
  }

  function ask(kind: Action['kind'], user: UserRow): void {
    editing = null;
    action = { kind, user };
    resetPw = '';
    actionError = null;
  }

  async function confirm(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    const a = action;
    if (!a || busy) return;
    busy = true;
    actionError = null;
    try {
      if (a.kind === 'delete') {
        await api.userDelete(a.user.id);
        showToast(t('users.deleted', undefined, { login: a.user.login }));
      } else if (a.kind === 'reset2fa') {
        await api.userReset2fa(a.user.id);
        showToast(t('users.reset2faDone'));
      } else {
        await api.userSetPassword(a.user.id, resetPw);
        showToast(t('users.passwordSet'));
      }
      action = null;
      await load();
    } catch (e) {
      if (isUnauthorized(e)) onauthlost();
      else actionError = errText(e, t('common.genericError'));
    } finally {
      busy = false;
    }
  }
</script>

<div class="users">
  <header class="head">
    <h3>{$tr('users.title')}</h3>
    <button class="btn primary" type="button" onclick={() => (adding = !adding)}>{$tr('users.add')}</button>
  </header>
  <p class="muted">{$tr('users.hint')}</p>

  {#if adding}
    <form class="card stack" onsubmit={create}>
      <label class="field">
        <span class="label">{$tr('users.login')}</span>
        <input class="input" type="text" name="new-login" autocomplete="off" autocapitalize="none" spellcheck="false" required bind:value={newLogin} />
      </label>
      <label class="field">
        <span class="label">{$tr('users.password')}</span>
        <input class="input" type="password" name="new-password" autocomplete="new-password" required bind:value={newPassword} />
      </label>
      <AccessEditor bind:role={newRole} bind:access={newAccess} />
      {#if addError}<p class="error" role="alert">{addError}</p>{/if}
      <div class="row end">
        <button class="btn" type="button" onclick={() => (adding = false)}>{$tr('common.cancel')}</button>
        <button class="btn primary" type="submit" disabled={busy}>{$tr('users.create')}</button>
      </div>
    </form>
  {/if}

  {#if failed}
    <p class="error" role="alert">{$tr('users.loadFailed')}</p>
  {:else if !loaded}
    <p class="muted">{$tr('common.loading')}</p>
  {:else}
    <ul class="list">
      {#each users as u (u.id)}
        <li class="user">
          <div class="info">
            <strong>{u.login}</strong>
            {#if u.current}<span class="badge you">{$tr('users.you')}</span>{/if}
            <span class="badge" class:accent={u.role !== 'admin'}>{$tr(`users.role.${u.role}`)}</span>
            {#if u.access}<span class="muted small">{summary(u)}</span>{/if}
            <span class="badge" class:good={u.totp_enabled}>{u.totp_enabled ? $tr('users.2fa.on') : $tr('users.2fa.off')}</span>
            {#if u.passkeys > 0}<span class="badge good">{$tr('users.passkeys', { n: u.passkeys })}</span>{/if}
            <span class="muted small">{$tr('users.created', { date: formatDateTime(u.created_at, $locale) })}</span>
          </div>
          {#if !u.current && $isPhone}
            <RowMenu label={$tr('docker.menu')} title={u.login} items={menu(u)} />
          {:else if !u.current}
            <div class="row acts">
              <button class="btn sm" type="button" onclick={() => startEdit(u)}>{$tr('users.access')}</button>
              <button class="btn sm" type="button" onclick={() => ask('password', u)}>{$tr('users.reset')}</button>
              <button class="btn sm" type="button" disabled={!u.totp_enabled && u.passkeys === 0} onclick={() => ask('reset2fa', u)}>{$tr('users.reset2fa')}</button>
              <button class="btn sm danger" type="button" onclick={() => ask('delete', u)}>{$tr('users.delete')}</button>
            </div>
          {/if}
          {#if editing && editing.user.id === u.id}
            <div class="confirm stack">
              <AccessEditor bind:role={editing.role} bind:access={editing.access} />
              {#if actionError}<p class="error" role="alert">{actionError}</p>{/if}
              <div class="row end">
                <button class="btn" type="button" onclick={() => (editing = null)}>{$tr('common.cancel')}</button>
                <button class="btn primary" type="button" disabled={busy} onclick={() => void saveEdit()}>{$tr('acc.apply')}</button>
              </div>
            </div>
          {/if}
          {#if action && action.user.id === u.id}
            <form class="confirm stack" onsubmit={confirm}>
              {#if action.kind === 'delete'}
                <p>{$tr('users.confirmDelete', { login: u.login })}</p>
              {:else if action.kind === 'reset2fa'}
                <p>{$tr('users.confirmReset2fa', { login: u.login })}</p>
              {:else}
                <label class="field">
                  <span class="label">{$tr('users.newPassword', { login: u.login })}</span>
                  <input class="input" type="password" autocomplete="new-password" required bind:value={resetPw} />
                </label>
                <p class="muted small">{$tr('users.confirmPassword')}</p>
              {/if}
              {#if actionError}<p class="error" role="alert">{actionError}</p>{/if}
              <div class="row end">
                <button class="btn" type="button" onclick={() => (action = null)}>{$tr('common.cancel')}</button>
                <button class="btn" class:danger={action.kind !== 'password'} class:primary={action.kind === 'password'} type="submit" disabled={busy || (action.kind === 'password' && !resetPw)}>
                  {action.kind === 'delete' ? $tr('users.delete') : action.kind === 'reset2fa' ? $tr('users.reset2fa') : $tr('users.reset')}
                </button>
              </div>
            </form>
          {/if}
        </li>
      {/each}
    </ul>
  {/if}
</div>

<style>
  .users {
    display: flex;
    flex-direction: column;
    gap: 11px;
    min-width: 0;
  }

  .head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 11px;
  }

  h3,
  p {
    margin: 0;
  }

  .list {
    display: flex;
    flex-direction: column;
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .user {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 7px 11px;
    padding: 9px 0;
    border-bottom: 1px dashed var(--seg-off);
  }

  .info {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 5px 7px;
    min-width: 0;
  }

  .badge {
    padding: 1px 5px;
    border: 2px solid var(--muted);
    color: var(--muted);
    font-size: 11px;
    white-space: nowrap;
  }

  .badge.good {
    border-color: var(--good);
    color: var(--good);
  }

  .badge.accent {
    border-color: var(--accent);
    color: var(--accent);
  }

  .badge.you {
    border-color: var(--accent);
    color: var(--accent);
  }

  .acts {
    gap: 5px;
  }

  .confirm {
    flex: 1 0 100%;
    padding: 11px;
    border: 2px solid var(--line);
  }

  /* Phones: every account is a card, its actions are in a sheet, the form buttons are one under another. */
  @media (max-width: 720px) {
    .list {
      gap: 11px;
    }

    .user {
      flex-wrap: wrap;
      padding: 9px 5px 9px 11px;
      background: var(--surface);
      border: 2px solid var(--line);
      box-shadow: 3px 3px 0 var(--shade);
    }

    .info {
      flex: 1 1 0;
    }

    .confirm {
      margin-right: 5px;
    }

    .users :global(.row.end) {
      flex-direction: column-reverse;
      align-items: stretch;
    }

    .users :global(.row.end > .btn) {
      width: 100%;
    }
  }
</style>
