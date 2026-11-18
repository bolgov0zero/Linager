<script lang="ts">
  import { ApiError, winCall, type ServerDetail, type WinUser, type WinUsersState } from '../../../api';
  import { tr as T } from '../../../i18n';
  import Modal from '../../Modal.svelte';
  import ActionMenu from '../../ui/ActionMenu.svelte';
  import { showToast } from '../../toast';
  import WinConfirm from './WinConfirm.svelte';

  let { server }: { server: ServerDetail } = $props();
  const serverId = $derived(server.id);

  let data = $state<WinUsersState | null>(null);
  let loading = $state(true);
  let error = $state('');
  let busy = $state(false);

  type Dialog =
    | { kind: 'add' }
    | { kind: 'password'; user: WinUser }
    | { kind: 'groups'; user: WinUser }
    | { kind: 'confirm'; text: string; danger: boolean; run: () => Promise<void> };
  let dialog = $state<Dialog | null>(null);

  // form fields of the dialogs
  let fName = $state('');
  let fFull = $state('');
  let fPass = $state('');
  let fGroups = $state<string[]>([]);

  async function load(): Promise<void> {
    loading = true;
    try {
      data = await winCall<WinUsersState>(serverId, 'users', 'state');
      error = '';
    } catch (e) {
      error = message(e);
    } finally {
      loading = false;
    }
  }

  function message(e: unknown): string {
    return e instanceof ApiError && e.serverMessage ? e.serverMessage : $T('win.error');
  }

  $effect(() => {
    void serverId;
    void load();
  });

  /** Runs a dangerous action (the panel sends the confirmation the person gave in the dialog). */
  async function act(action: string, params: Record<string, unknown>): Promise<void> {
    busy = true;
    try {
      await winCall(serverId, 'users', action, params, true);
      dialog = null;
      await load();
    } catch (e) {
      showToast(message(e));
    } finally {
      busy = false;
    }
  }

  function ask(text: string, danger: boolean, run: () => Promise<void>): void {
    dialog = { kind: 'confirm', text, danger, run };
  }

  function openAdd(): void {
    fName = fFull = fPass = '';
    fGroups = data?.groups.includes('Users') ? ['Users'] : [];
    dialog = { kind: 'add' };
  }

  const toggleGroup = (g: string) => (fGroups = fGroups.includes(g) ? fGroups.filter((x) => x !== g) : [...fGroups, g]);

  // search and filters, like the Linux tab
  let q = $state('');
  let fEnabled = $state(false);
  let fAdmins = $state(false);
  let fOnline = $state(false);
  const HOT_GROUPS = ['Administrators', 'Администраторы'];
  const sessionsOf = (name: string) => (data?.sessions ?? []).filter((x) => x.user.toLowerCase() === name.toLowerCase()).length;
  const rows = $derived(
    (data?.users ?? []).filter((u) => {
      if (q.trim() && !`${u.name} ${u.full_name}`.toLowerCase().includes(q.trim().toLowerCase())) return false;
      if (fEnabled && !u.enabled) return false;
      if (fAdmins && !u.groups.some((g) => HOT_GROUPS.includes(g))) return false;
      if (fOnline && sessionsOf(u.name) === 0) return false;
      return true;
    }),
  );

  function menu(u: WinUser): { key: string; label: string; disabled?: boolean; onselect: () => void }[] {
    return [
      u.enabled
        ? { key: 'off', label: $T('wu.disable'), disabled: busy || u.builtin, onselect: () => ask($T('wu.confirm.disable', { name: u.name }), false, () => act('update', { name: u.name, enabled: false })) }
        : { key: 'on', label: $T('wu.enable'), disabled: busy, onselect: () => void act('update', { name: u.name, enabled: true }) },
      { key: 'pw', label: $T('wu.setPassword'), disabled: busy, onselect: () => { fPass = ''; dialog = { kind: 'password', user: u }; } },
      { key: 'groups', label: $T('wu.editGroups'), disabled: busy, onselect: () => { fGroups = [...u.groups]; dialog = { kind: 'groups', user: u }; } },
      { key: 'del', label: $T('wu.delete'), disabled: busy || u.builtin, onselect: () => ask($T('wu.confirm.delete', { name: u.name }), true, () => act('delete', { name: u.name })) },
    ];
  }

  const when = (t: number) => (t > 0 ? new Date(t * 1000).toLocaleString($T('win.yes') === 'да' ? 'ru-RU' : 'en-GB') : $T('wu.never'));
</script>

<div class="wrap">
  <div class="tools">
    <input class="input search" type="search" bind:value={q} placeholder={$T('wu.search')} aria-label={$T('wu.search')} />
    <button class="chip" type="button" aria-pressed={fEnabled} onclick={() => (fEnabled = !fEnabled)}>{$T('wu.f.enabled')}</button>
    <button class="chip" type="button" aria-pressed={fAdmins} onclick={() => (fAdmins = !fAdmins)}>{$T('wu.f.admins')}</button>
    <button class="chip" type="button" aria-pressed={fOnline} onclick={() => (fOnline = !fOnline)}>{$T('wu.f.online')}</button>
    <button class="btn" type="button" onclick={() => void load()}>{$T('win.refresh')}</button>
    <button class="btn primary add" type="button" onclick={openAdd}>{$T('wu.add')}</button>
  </div>

  {#if loading && !data}
    <p class="muted">{$T('win.loading')}</p>
  {:else if error}
    <p class="err" role="alert">{error}</p>
  {:else if data}
    <div class="table-wrap">
      <table class="tbl">
        <thead>
          <tr>
            <th scope="col">{$T('wu.name')}</th>
            <th scope="col">{$T('wu.state')}</th>
            <th scope="col">{$T('wu.groups')}</th>
            <th scope="col">{$T('wu.lastLogon')}</th>
            <th scope="col">{$T('wu.now')}</th>
            <th scope="col"><span class="sr">{$T('wu.actions')}</span></th>
          </tr>
        </thead>
        <tbody>
          {#each rows as u (u.sid)}
            <tr class="u">
              <td class="nw">
                <div class="uname">{u.name}{#if u.builtin}<span class="badge tone-muted">{$T('wu.builtin')}</span>{/if}</div>
                <div class="muted small">{u.full_name || u.description}</div>
              </td>
              <td><span class="badge" class:tone-ok={u.enabled} class:tone-muted={!u.enabled}>{u.enabled ? $T('wu.enabled') : $T('wu.disabled')}</span></td>
              <td><div class="gl">{#each u.groups as g (g)}<span class="gtag" class:hot={HOT_GROUPS.includes(g)}>{g}</span>{:else}<span class="muted">—</span>{/each}</div></td>
              <td class="nw">{u.last_logon > 0 ? when(u.last_logon) : $T('wu.never')}</td>
              <td class="nw">{#if sessionsOf(u.name) > 0}<span class="tone-ok"><span class="dot"></span>{$T('wu.sessionsN', { n: sessionsOf(u.name) })}</span>{:else}<span class="muted">—</span>{/if}</td>
              <td class="acts"><div class="act-row"><ActionMenu label={$T('wu.menu')} items={menu(u)} /></div></td>
            </tr>
          {:else}
            <tr><td colspan="6" class="muted empty">{$T('wu.empty')}</td></tr>
          {/each}
        </tbody>
      </table>
    </div>
    <p class="muted small">{$T('wu.legend', { shown: rows.length, total: data.users.length })}</p>

    <h3>{$T('wu.sessions')}</h3>
    {#if data.sessions.length === 0}
      <p class="muted">{$T('wu.noSessions')}</p>
    {:else}
      <div class="table-wrap">
        <table class="tbl">
          <thead><tr><th scope="col">{$T('wu.name')}</th><th scope="col">{$T('wu.sessionType')}</th><th scope="col">{$T('wu.state')}</th><th scope="col">ID</th><th scope="col"><span class="sr">{$T('wu.actions')}</span></th></tr></thead>
          <tbody>
            {#each data.sessions as s (s.id)}
              <tr>
                <td class="nw"><b>{s.user}</b></td>
                <td>{$T(`wu.type.${s.type}`)}</td>
                <td><span class="badge" class:tone-ok={s.state === 'active'} class:tone-muted={s.state !== 'active'}>{s.state}</span></td>
                <td class="mono">#{s.id}</td>
                <td class="acts"><div class="act-row"><button class="btn sm crit" type="button" disabled={busy} onclick={() => ask($T('wu.confirm.logoff', { name: s.user }), true, () => act('logoff', { id: s.id }))}>{$T('wu.logoff')}</button></div></td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {/if}
  {/if}
</div>

{#if dialog?.kind === 'add'}
  <Modal title={$T('wu.addTitle')} labelId="wu-add-title" onclose={() => (dialog = null)}>
    <form class="form" onsubmit={(e) => { e.preventDefault(); void act('add', { name: fName.trim(), full_name: fFull, password: fPass, groups: fGroups }); }}>
      <label><span>{$T('wu.name')}</span><input bind:value={fName} maxlength="20" autocomplete="off" spellcheck="false" /></label>
      <label><span>{$T('wu.fullName')}</span><input bind:value={fFull} autocomplete="off" /></label>
      <label><span>{$T('wu.password')}</span><input type="password" bind:value={fPass} autocomplete="new-password" /></label>
      <p class="small muted">{$T('wu.pwHint')}</p>
      <fieldset><legend>{$T('wu.groups')}</legend>
        {#each data?.groups ?? [] as g (g)}<label class="chk"><input type="checkbox" checked={fGroups.includes(g)} onchange={() => toggleGroup(g)} /> {g}</label>{/each}
      </fieldset>
      <div class="row end"><button class="btn" type="button" onclick={() => (dialog = null)}>{$T('win.cancel')}</button><button class="btn primary" type="submit" disabled={busy || !fName.trim() || !fPass}>{$T('wu.add')}</button></div>
    </form>
  </Modal>
{:else if dialog?.kind === 'password'}
  {@const user = dialog.user}
  <Modal title={user.name} labelId="wu-pw-title" onclose={() => (dialog = null)}>
    <form class="form" onsubmit={(e) => { e.preventDefault(); void act('passwd', { name: user.name, password: fPass }); }}>
      <label><span>{$T('wu.newPassword')}</span><input type="password" bind:value={fPass} autocomplete="new-password" /></label>
      <div class="row end"><button class="btn" type="button" onclick={() => (dialog = null)}>{$T('win.cancel')}</button><button class="btn primary" type="submit" disabled={busy || !fPass}>{$T('win.save')}</button></div>
    </form>
  </Modal>
{:else if dialog?.kind === 'groups'}
  {@const user = dialog.user}
  <Modal title={$T('wu.groupsTitle', { name: user.name })} labelId="wu-groups-title" onclose={() => (dialog = null)}>
    <div class="form">
      <fieldset>
        {#each data?.groups ?? [] as g (g)}<label class="chk"><input type="checkbox" checked={fGroups.includes(g)} onchange={() => toggleGroup(g)} /> {g}</label>{/each}
      </fieldset>
      <div class="row end"><button class="btn" type="button" onclick={() => (dialog = null)}>{$T('win.cancel')}</button><button class="btn primary" type="button" disabled={busy} onclick={() => void act('update', { name: user.name, groups: fGroups })}>{$T('win.save')}</button></div>
    </div>
  </Modal>
{:else if dialog?.kind === 'confirm'}
  {@const run = dialog.run}
  <WinConfirm text={dialog.text} danger={dialog.danger} onyes={() => void run()} onclose={() => (dialog = null)} />
{/if}

<style>
  .wrap { display: flex; flex-direction: column; gap: 10px; }
  h3 { margin: 6px 0; }
  .tools { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }
  .search { flex: 1 1 200px; max-width: 320px; height: 36px; padding-block: 0; }
  .add { margin-left: auto; }
  .table-wrap { overflow-x: auto; }
  .tbl { width: 100%; min-width: 760px; border-collapse: collapse; font-size: 13px; }
  .tbl th { padding: 6px 8px; text-align: left; font-family: var(--font-display); font-weight: 400; font-size: 12px; color: var(--muted); border-bottom: 2px solid var(--line); white-space: nowrap; }
  .tbl td { padding: 8px; vertical-align: middle; border-bottom: 1px dashed var(--seg-off); }
  .tbl tr.u:hover td { background: var(--seg-off); }
  .nw { white-space: nowrap; }
  .uname { display: flex; align-items: center; gap: 8px; font-weight: 600; white-space: nowrap; }
  .gl { display: flex; flex-wrap: wrap; gap: 4px; }
  .gtag { padding: 0 6px; white-space: nowrap; border: 1px solid var(--muted); color: var(--muted); font-size: 12px; }
  .gtag.hot { border-color: var(--warn); color: var(--warn); }
  .badge { display: inline-block; padding: 0 6px; border: 2px solid currentColor; font-size: 12px; line-height: 1.35; white-space: nowrap; }
  .tone-ok { color: var(--good); }
  .tone-muted { color: var(--muted); }
  .dot { display: inline-block; width: 8px; height: 8px; margin-right: 6px; background: currentColor; }
  .acts { width: 1%; white-space: nowrap; }
  .act-row { display: flex; justify-content: flex-end; gap: 6px; }
  .sr { position: absolute; width: 1px; height: 1px; overflow: hidden; clip: rect(0 0 0 0); }
  .empty { text-align: center; }
  /* a row button of a dangerous action: an outline, the solid red one is for the confirmation */
  .crit { background: var(--surface); color: var(--crit); border-color: var(--crit); }
  .crit:disabled { color: var(--muted); border-color: var(--line); }
  .err { color: var(--crit); }
  .form { display: flex; flex-direction: column; gap: 10px; min-width: min(360px, 80vw); }
  .form label { display: flex; flex-direction: column; gap: 4px; font-size: 13px; }
  .form label.chk { flex-direction: row; align-items: center; gap: 6px; }
  .form input:not([type='checkbox']) { padding: 8px 10px; font: inherit; color: var(--ink); background: var(--bg); border: 2px solid var(--line); }
  .row { display: flex; gap: 8px; flex-wrap: wrap; }
  .row.end { justify-content: flex-end; }
  fieldset { border: 2px solid var(--line); padding: 8px 10px; display: flex; flex-direction: column; gap: 4px; max-height: 220px; overflow: auto; }
  p { margin: 0; }
</style>
