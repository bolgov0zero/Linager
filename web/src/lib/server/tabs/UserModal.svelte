<script lang="ts">
  import { untrack } from 'svelte';
  import { api, ApiError, type HostUser, type HostUserPatch, type HostUsersState } from '../../../api';
  import { locale, t, tr } from '../../../i18n';
  import { showToast } from '../../toast';
  import Modal from '../../Modal.svelte';
  import Select from '../../ui/Select.svelte';
  import { formatDateTime } from '../format';
  import { errorText } from './svc';
  import { HOT_GROUPS, lastAdmin } from './users-util';

  let {
    serverId,
    user,
    info,
    onclose,
    ondone,
    ondelete,
  }: {
    serverId: string;
    user: HostUser;
    info: HostUsersState;
    onclose: () => void;
    /** The account was changed: the list is reloaded. */
    ondone: () => void;
    ondelete: (u: HostUser) => void;
  } = $props();

  // The draft starts from the account as it is now; only what the administrator changed is sent.
  // Taken once, when the dialog opens: the list is refreshed in the background and must not reset the draft.
  const start = untrack(() => ({
    sudo: (user.sudo === 'root' ? 'none' : user.sudo) as 'none' | 'pw' | 'nopw',
    groups: info.groups.filter((g) => user.groups.includes(g)),
    shell: user.shell,
    locked: user.locked,
  }));
  let sudo = $state(start.sudo);
  let groups = $state(start.groups);
  let shell = $state(start.shell);
  let locked = $state(start.locked);

  let busy = $state(false);
  let error = $state<string | null>(null);

  const readonlyAccount = $derived(!user.human);
  const wasAdmin = $derived(user.sudo !== 'none' && user.sudo !== 'root');
  const lastOne = $derived(lastAdmin(info, user.name));
  const dropsAdmin = $derived((wasAdmin && sudo === 'none') || (wasAdmin && locked && !user.locked));
  const blocked = $derived(dropsAdmin && lastOne);

  const shellOptions = $derived(
    [...new Set([user.shell, ...info.shells])].map((s) => ({ value: s, label: s })),
  );

  const patch = $derived.by<HostUserPatch>(() => {
    const p: HostUserPatch = {};
    const was = start.groups;
    if (groups.length !== was.length || groups.some((g) => !was.includes(g))) p.groups = groups;
    if (sudo !== start.sudo) p.sudo = sudo;
    if (shell !== start.shell) p.shell = shell;
    if (locked !== start.locked) p.locked = locked;
    return p;
  });
  const changed = $derived(Object.keys(patch).length > 0);

  function toggleGroup(g: string): void {
    groups = groups.includes(g) ? groups.filter((x) => x !== g) : [...groups, g];
  }

  async function apply(): Promise<void> {
    if (busy || !changed || blocked) return;
    busy = true;
    error = null;
    try {
      await api.hostUserUpdate(serverId, user.name, patch);
      showToast(t('usr.saved', undefined, { user: user.name }));
      ondone();
    } catch (e) {
      error = e instanceof ApiError && e.serverMessage ? e.serverMessage : errorText(e, (k, v) => t(k, undefined, v));
    } finally {
      busy = false;
    }
  }
</script>

<Modal title={$tr('usr.props.title', { user: user.name })} labelId="usr-props" {onclose}>
  <div class="stack">
    <section class="sec">
      <h3>{$tr('usr.props.account')}</h3>
      <dl class="kv">
        <dt>UID / GID</dt><dd>{user.uid} / {user.gid}</dd>
        {#if user.gecos}<dt>{$tr('usr.props.gecos')}</dt><dd>{user.gecos}</dd>{/if}
        <dt>{$tr('usr.props.home')}</dt><dd class="mono">{user.home}</dd>
        <dt>{$tr('usr.props.password')}</dt>
        <dd>
          {user.has_password ? $tr('usr.props.pwSet') : $tr('usr.props.pwNone')}
          {#if user.has_password && user.pw_changed > 0}<span class="muted"> · {$tr('usr.props.pwChanged', { date: formatDateTime(user.pw_changed, $locale) })}</span>{/if}
        </dd>
        <dt>{$tr('usr.props.keys')}</dt><dd>{user.keys}</dd>
        <dt>{$tr('usr.props.now')}</dt>
        <dd>{user.sessions > 0 ? $tr('usr.props.sessions', { s: user.sessions, p: user.procs }) : $tr('usr.props.noSessions', { p: user.procs })}</dd>
      </dl>
    </section>

    {#if readonlyAccount}
      <p class="muted">{$tr('usr.props.system')}</p>
    {:else}
      <section class="sec">
        <h3>{$tr('usr.props.sudo')}</h3>
        <div class="opts" role="radiogroup" aria-label={$tr('usr.props.sudo')}>
          {#each [['none', 'usr.sudo.none', 'usr.sudo.noneHint'], ['pw', 'usr.sudo.pw', 'usr.sudo.pwHint'], ['nopw', 'usr.sudo.nopw', 'usr.sudo.nopwHint']] as [v, label, hint] (v)}
            <label class="opt" class:on={sudo === v}>
              <input type="radio" bind:group={sudo} value={v} />
              <span>{$tr(label)}<small class="muted" class:crit={v === 'nopw'}>{$tr(hint)}</small></span>
            </label>
          {/each}
        </div>
        {#if blocked}<p class="warnbox">{$tr('usr.props.lastAdmin')}</p>{/if}
      </section>

      <section class="sec">
        <h3>{$tr('usr.props.groups')}</h3>
        <div class="checks">
          {#each info.groups as g (g)}
            <label class="chk" class:on={groups.includes(g)}>
              <input type="checkbox" checked={groups.includes(g)} onchange={() => toggleGroup(g)} />
              <span>{g}</span>
              {#if HOT_GROUPS.includes(g)}<span class="hint-hot">{$tr('usr.props.hot')}</span>{/if}
            </label>
          {/each}
        </div>
      </section>

      <section class="sec">
        <h3>{$tr('usr.props.login')}</h3>
        <div class="field">
          <span class="label">{$tr('usr.props.shell')}</span>
          <Select bind:value={shell} options={shellOptions} ariaLabel={$tr('usr.props.shell')} />
        </div>
        <label class="check lock">
          <input type="checkbox" bind:checked={locked} />
          <span>{$tr('usr.props.lock')}<br /><span class="muted small">{$tr('usr.props.lockHint')}</span></span>
        </label>
      </section>
    {/if}

    <p class="muted small">{$tr('usr.props.audit')}</p>
    {#if error}<p class="error" role="alert">{error}</p>{/if}
    <div class="foot">
      <button class="btn danger" type="button" disabled={user.protected || !user.human} onclick={() => ondelete(user)}>{$tr('usr.props.delete')}</button>
      <span class="spacer"></span>
      <button class="btn primary" type="button" disabled={busy || !changed || blocked || readonlyAccount} onclick={() => void apply()}>{$tr('usr.props.apply')}</button>
      <button class="btn" type="button" onclick={onclose}>{$tr('common.cancel')}</button>
    </div>
  </div>
</Modal>

<style>
  .sec h3 {
    margin: 0 0 8px;
    font-family: var(--font-display);
    font-weight: 400;
    font-size: 13px;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--muted);
  }

  .kv {
    display: grid;
    grid-template-columns: 130px 1fr;
    gap: 4px 12px;
    margin: 0;
    font-size: 13px;
  }

  .kv dt {
    color: var(--muted);
  }

  .kv dd {
    margin: 0;
    overflow-wrap: anywhere;
  }

  .opts {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .opt {
    display: flex;
    gap: 10px;
    align-items: flex-start;
    padding: 8px 10px;
    border: 2px solid var(--seg-off);
    cursor: pointer;
  }

  .opt.on {
    border-color: var(--line);
  }

  .opt small {
    display: block;
    font-size: 12px;
  }

  .opt small.crit {
    color: var(--crit);
  }

  .checks {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 6px;
  }

  .chk {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 8px;
    border: 2px solid var(--seg-off);
    cursor: pointer;
    font-size: 13px;
  }

  .chk.on {
    border-color: var(--line);
  }

  .hint-hot {
    margin-left: auto;
    color: var(--warn);
    font-size: 11px;
  }

  .lock {
    margin-top: 10px;
  }

  .warnbox {
    margin: 8px 0 0;
    padding: 8px 10px;
    border: 2px solid var(--warn);
    color: var(--warn);
    font-size: 12px;
  }

  .foot {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
    padding-top: 12px;
    border-top: 2px solid var(--line);
  }

  .spacer {
    flex: 1;
  }

  @media (max-width: 600px) {
    .checks {
      grid-template-columns: 1fr;
    }

    .kv {
      grid-template-columns: 1fr;
    }
  }

  @media (max-width: 720px) {
    .kv {
      grid-template-columns: 110px minmax(0, 1fr);
    }

    .chk,
    .opt {
      min-height: 48px;
      align-items: center;
    }

    .foot {
      position: sticky;
      bottom: 0;
      z-index: 2;
      margin: 0 -12px calc(-12px - var(--safe-b));
      padding: 10px 12px calc(10px + var(--safe-b));
      background: var(--surface);
    }

    .spacer {
      display: none;
    }

    .foot > .btn {
      flex: 1 1 40%;
    }

    .foot > .btn.danger {
      flex: 1 0 100%;
      order: -1;
    }
  }
</style>
