<script lang="ts">
  import { ApiError, winCall, type ServerDetail, type WinUser, type WinUsersState } from '../../../api';
  import { tr } from '../../../i18n';
  import Modal from '../../Modal.svelte';
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
    return e instanceof ApiError && e.serverMessage ? e.serverMessage : $tr('win.error');
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

  const when = (t: number) => (t > 0 ? new Date(t * 1000).toLocaleString($tr('win.yes') === 'да' ? 'ru-RU' : 'en-GB') : $tr('wu.never'));
</script>

<div class="wrap">
  <div class="bar">
    <h3>{$tr('wu.title')}</h3>
    <div class="row">
      <button class="btn" type="button" onclick={() => void load()}>{$tr('win.refresh')}</button>
      <button class="btn primary" type="button" onclick={openAdd}>{$tr('wu.add')}</button>
    </div>
  </div>

  {#if loading && !data}
    <p class="muted">{$tr('win.loading')}</p>
  {:else if error}
    <p class="err" role="alert">{error}</p>
  {:else if data}
    <div class="table">
      {#each data.users as u (u.sid)}
        <div class="card urow" class:off={!u.enabled}>
          <div class="who">
            <b>{u.name}</b>
            {#if u.full_name}<span class="muted"> · {u.full_name}</span>{/if}
            <div class="small muted">
              {u.enabled ? $tr('wu.enabled') : $tr('wu.disabled')}{u.builtin ? ` · ${$tr('wu.builtin')}` : ''} · {$tr('wu.lastLogon')}: {when(u.last_logon)}
            </div>
            <div class="chips">{#each u.groups as g (g)}<span class="chip">{g}</span>{/each}</div>
          </div>
          <div class="acts">
            <button class="btn sm" type="button" disabled={busy || (u.builtin && u.enabled)} onclick={() => (u.enabled ? ask($tr('wu.confirm.disable', { name: u.name }), false, () => act('update', { name: u.name, enabled: false })) : void act('update', { name: u.name, enabled: true }))}>{u.enabled ? $tr('wu.disable') : $tr('wu.enable')}</button>
            <button class="btn sm" type="button" disabled={busy} onclick={() => { fPass = ''; dialog = { kind: 'password', user: u }; }}>{$tr('wu.setPassword')}</button>
            <button class="btn sm" type="button" disabled={busy} onclick={() => { fGroups = [...u.groups]; dialog = { kind: 'groups', user: u }; }}>{$tr('wu.editGroups')}</button>
            <button class="btn sm danger" type="button" disabled={busy || u.builtin} onclick={() => ask($tr('wu.confirm.delete', { name: u.name }), true, () => act('delete', { name: u.name }))}>{$tr('wu.delete')}</button>
          </div>
        </div>
      {/each}
    </div>

    <h3>{$tr('wu.sessions')}</h3>
    {#if data.sessions.length === 0}
      <p class="muted">{$tr('wu.noSessions')}</p>
    {:else}
      {#each data.sessions as s (s.id)}
        <div class="card urow">
          <div class="who"><b>{s.user}</b> <span class="muted small">· {$tr(`wu.type.${s.type}`)} · {s.state} · #{s.id}</span></div>
          <button class="btn sm danger" type="button" disabled={busy} onclick={() => ask($tr('wu.confirm.logoff', { name: s.user }), true, () => act('logoff', { id: s.id }))}>{$tr('wu.logoff')}</button>
        </div>
      {/each}
    {/if}
  {/if}
</div>

{#if dialog?.kind === 'add'}
  <Modal title={$tr('wu.addTitle')} labelId="wu-add-title" onclose={() => (dialog = null)}>
    <form class="form" onsubmit={(e) => { e.preventDefault(); void act('add', { name: fName.trim(), full_name: fFull, password: fPass, groups: fGroups }); }}>
      <label><span>{$tr('wu.name')}</span><input bind:value={fName} maxlength="20" autocomplete="off" spellcheck="false" /></label>
      <label><span>{$tr('wu.fullName')}</span><input bind:value={fFull} autocomplete="off" /></label>
      <label><span>{$tr('wu.password')}</span><input type="password" bind:value={fPass} autocomplete="new-password" /></label>
      <p class="small muted">{$tr('wu.pwHint')}</p>
      <fieldset><legend>{$tr('wu.groups')}</legend>
        {#each data?.groups ?? [] as g (g)}<label class="chk"><input type="checkbox" checked={fGroups.includes(g)} onchange={() => toggleGroup(g)} /> {g}</label>{/each}
      </fieldset>
      <div class="row end"><button class="btn" type="button" onclick={() => (dialog = null)}>{$tr('win.cancel')}</button><button class="btn primary" type="submit" disabled={busy || !fName.trim() || !fPass}>{$tr('wu.add')}</button></div>
    </form>
  </Modal>
{:else if dialog?.kind === 'password'}
  {@const user = dialog.user}
  <Modal title={user.name} labelId="wu-pw-title" onclose={() => (dialog = null)}>
    <form class="form" onsubmit={(e) => { e.preventDefault(); void act('passwd', { name: user.name, password: fPass }); }}>
      <label><span>{$tr('wu.newPassword')}</span><input type="password" bind:value={fPass} autocomplete="new-password" /></label>
      <div class="row end"><button class="btn" type="button" onclick={() => (dialog = null)}>{$tr('win.cancel')}</button><button class="btn primary" type="submit" disabled={busy || !fPass}>{$tr('win.save')}</button></div>
    </form>
  </Modal>
{:else if dialog?.kind === 'groups'}
  {@const user = dialog.user}
  <Modal title={$tr('wu.groupsTitle', { name: user.name })} labelId="wu-groups-title" onclose={() => (dialog = null)}>
    <div class="form">
      <fieldset>
        {#each data?.groups ?? [] as g (g)}<label class="chk"><input type="checkbox" checked={fGroups.includes(g)} onchange={() => toggleGroup(g)} /> {g}</label>{/each}
      </fieldset>
      <div class="row end"><button class="btn" type="button" onclick={() => (dialog = null)}>{$tr('win.cancel')}</button><button class="btn primary" type="button" disabled={busy} onclick={() => void act('update', { name: user.name, groups: fGroups })}>{$tr('win.save')}</button></div>
    </div>
  </Modal>
{:else if dialog?.kind === 'confirm'}
  {@const run = dialog.run}
  <WinConfirm text={dialog.text} danger={dialog.danger} onyes={() => void run()} onclose={() => (dialog = null)} />
{/if}

<style>
  .wrap { display: flex; flex-direction: column; gap: 10px; }
  .bar { display: flex; align-items: center; justify-content: space-between; gap: 10px; flex-wrap: wrap; }
  h3 { margin: 6px 0; }
  .row { display: flex; gap: 8px; flex-wrap: wrap; }
  .row.end { justify-content: flex-end; }
  .table { display: flex; flex-direction: column; gap: 8px; }
  .urow { display: flex; align-items: center; justify-content: space-between; gap: 10px; padding: 10px 12px; flex-wrap: wrap; }
  .urow.off { opacity: 0.7; }
  .who { min-width: 0; }
  .acts { display: flex; gap: 6px; flex-wrap: wrap; }
  .chips { display: flex; gap: 4px; flex-wrap: wrap; margin-top: 4px; }
  .chip { padding: 1px 6px; border: 2px solid var(--line); font-size: 12px; }
  .danger { color: var(--crit); border-color: var(--crit); }
  .err { color: var(--crit); }
  .form { display: flex; flex-direction: column; gap: 10px; min-width: min(360px, 80vw); }
  .form label { display: flex; flex-direction: column; gap: 4px; font-size: 13px; }
  .form label.chk { flex-direction: row; align-items: center; gap: 6px; }
  .form input:not([type='checkbox']) { padding: 8px 10px; font: inherit; color: var(--ink); background: var(--bg); border: 2px solid var(--line); }
  fieldset { border: 2px solid var(--line); padding: 8px 10px; display: flex; flex-direction: column; gap: 4px; max-height: 220px; overflow: auto; }
  p { margin: 0; }
</style>
