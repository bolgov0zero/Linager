<script lang="ts">
  import { api, ApiError, type HostUsersState } from '../../../api';
  import { t, tr } from '../../../i18n';
  import { showToast } from '../../toast';
  import Modal from '../../Modal.svelte';
  import Select from '../../ui/Select.svelte';
  import { errorText } from './svc';
  import { genPassword, HOT_GROUPS } from './users-util';

  let {
    serverId,
    info,
    onclose,
    ondone,
  }: { serverId: string; info: HostUsersState; onclose: () => void; ondone: () => void } = $props();

  let name = $state('');
  let gecos = $state('');
  let shell = $state('/bin/bash');
  let home = $state('');
  let pwMode = $state<'generate' | 'manual' | 'none'>('generate');
  let generated = $state(genPassword());
  let manual = $state('');
  let groups = $state<string[]>([]);
  let sudo = $state<'none' | 'pw' | 'nopw'>('none');
  let sshKey = $state('');
  let busy = $state(false);
  let error = $state<string | null>(null);

  const nameOk = $derived(/^[a-z_][a-z0-9_-]{0,31}$/.test(name));
  const taken = $derived(info.users.some((u) => u.name === name));
  const password = $derived(pwMode === 'generate' ? generated : pwMode === 'manual' ? manual : '');
  const pwOk = $derived(pwMode === 'none' || (password.length >= 8 && password.length <= 128));
  const shellOptions = $derived(info.shells.map((s) => ({ value: s, label: s })));
  const homePath = $derived(home || `/home/${name || 'login'}`);

  /** The commands the agent will run, shown before the account is created (the password is not part of them). */
  const preview = $derived.by(() => {
    const all = sudo === 'none' ? groups : [...groups, info.sudo_group];
    const lines = [
      `useradd --create-home --home-dir ${homePath} --shell ${shell} --user-group${gecos ? ` --comment "${gecos}"` : ''}${all.length ? ` --groups ${all.join(',')}` : ''} ${name || 'login'}`,
    ];
    if (password) lines.push('chpasswd   # ' + t('usr.add.previewPw'));
    if (sudo === 'nopw') lines.push(`/etc/sudoers.d/linager-${name || 'login'}   # NOPASSWD: ALL`);
    if (sshKey.trim()) lines.push(`${homePath}/.ssh/authorized_keys`);
    return lines.join('\n');
  });

  function toggleGroup(g: string): void {
    groups = groups.includes(g) ? groups.filter((x) => x !== g) : [...groups, g];
  }

  async function copy(): Promise<void> {
    try {
      await navigator.clipboard.writeText(generated);
      showToast(t('usr.pw.copied'));
    } catch {
      // a blocked clipboard: the password stays on screen
    }
  }

  async function create(): Promise<void> {
    if (busy || !nameOk || taken || !pwOk) return;
    busy = true;
    error = null;
    try {
      const res = await api.hostUserAdd(serverId, { name, gecos, shell, home, groups, sudo, password, ssh_key: sshKey.trim() });
      showToast(res.warnings.length > 0 ? t('usr.add.doneWarn', undefined, { user: name, w: res.warnings.join('; ') }) : t('usr.add.done', undefined, { user: name }));
      ondone();
    } catch (e) {
      error = e instanceof ApiError && e.serverMessage ? e.serverMessage : errorText(e, (k, v) => t(k, undefined, v));
    } finally {
      busy = false;
    }
  }
</script>

<Modal title={$tr('usr.add.title')} labelId="usr-add" medium {onclose}>
  <form class="stack" onsubmit={(e) => { e.preventDefault(); void create(); }}>
    <div class="cols">
      <div class="col">
        <div class="two">
          <label class="field">
            <span class="label">{$tr('usr.add.login')}</span>
            <input class="input" bind:value={name} autocomplete="off" spellcheck="false" autocapitalize="none" />
            {#if name && !nameOk}<span class="error small">{$tr('usr.add.loginRule')}</span>{:else if taken}<span class="error small">{$tr('usr.add.taken')}</span>{/if}
          </label>
          <label class="field">
            <span class="label">{$tr('usr.add.gecos')}</span>
            <input class="input" bind:value={gecos} maxlength="100" />
          </label>
        </div>
        <div class="two">
          <div class="field">
            <span class="label">{$tr('usr.props.shell')}</span>
            <Select bind:value={shell} options={shellOptions} ariaLabel={$tr('usr.props.shell')} />
          </div>
          <label class="field">
            <span class="label">{$tr('usr.props.home')}</span>
            <input class="input mono" bind:value={home} placeholder={`/home/${name || 'login'}`} spellcheck="false" />
          </label>
        </div>
        <div class="field">
          <span class="label">{$tr('usr.props.sudo')}</span>
          <div class="row">
            <button class="chip" type="button" aria-pressed={sudo === 'none'} onclick={() => (sudo = 'none')}>{$tr('usr.sudo.none')}</button>
            <button class="chip" type="button" aria-pressed={sudo === 'pw'} onclick={() => (sudo = 'pw')}>{$tr('usr.sudo.pw')}</button>
            <button class="chip" type="button" aria-pressed={sudo === 'nopw'} onclick={() => (sudo = 'nopw')}>{$tr('usr.sudo.nopw')}</button>
          </div>
          {#if sudo === 'nopw'}<span class="error small">{$tr('usr.sudo.nopwHint')}</span>{/if}
        </div>
        <div class="field">
          <span class="label">{$tr('usr.props.groups')}</span>
          <div class="row">
            {#each info.groups as g (g)}
              <button class="chip" type="button" aria-pressed={groups.includes(g)} onclick={() => toggleGroup(g)}>{g}{HOT_GROUPS.includes(g) ? ' ⚠' : ''}</button>
            {/each}
          </div>
        </div>
      </div>

      <div class="col">
        <div class="field">
          <span class="label">{$tr('usr.add.password')}</span>
          <div class="opts">
            <label class="opt" class:on={pwMode === 'generate'}><input type="radio" bind:group={pwMode} value="generate" /><span>{$tr('usr.pw.generate')}<small class="muted">{$tr('usr.pw.generateHint')}</small></span></label>
            <label class="opt" class:on={pwMode === 'manual'}><input type="radio" bind:group={pwMode} value="manual" /><span>{$tr('usr.pw.manual')}</span></label>
            <label class="opt" class:on={pwMode === 'none'}><input type="radio" bind:group={pwMode} value="none" /><span>{$tr('usr.add.pwNone')}<small class="muted">{$tr('usr.add.pwNoneHint')}</small></span></label>
          </div>
          {#if pwMode === 'generate'}
            <div class="row">
              <input class="input mono grow" readonly value={generated} aria-label={$tr('usr.pw.generated')} />
              <button class="btn sm" type="button" onclick={() => void copy()}>{$tr('common.copy')}</button>
              <button class="btn sm" type="button" onclick={() => (generated = genPassword())}>{$tr('usr.pw.again')}</button>
            </div>
            <span class="muted small">{$tr('usr.pw.once')}</span>
          {:else if pwMode === 'manual'}
            <input class="input" type="password" autocomplete="new-password" bind:value={manual} aria-label={$tr('usr.pw.new')} />
            <span class="muted small">{$tr('usr.pw.rule')}</span>
          {/if}
        </div>

        <label class="field">
          <span class="label">{$tr('usr.add.key')}</span>
          <textarea class="input mono" rows="2" bind:value={sshKey} placeholder="ssh-ed25519 AAAA… user@host" spellcheck="false"></textarea>
        </label>
      </div>
    </div>

    <div class="field">
      <span class="label">{$tr('usr.add.preview')}</span>
      <pre class="cmd">{preview}</pre>
    </div>

    {#if error}<p class="error" role="alert">{error}</p>{/if}
    <div class="row end">
      <button class="btn" type="button" onclick={onclose}>{$tr('common.cancel')}</button>
      <button class="btn primary" type="submit" disabled={busy || !nameOk || taken || !pwOk}>{$tr('usr.add.create')}</button>
    </div>
  </form>
</Modal>

<style>
  .cols {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 16px 24px;
    align-items: start;
  }

  .col {
    display: flex;
    flex-direction: column;
    gap: 12px;
    min-width: 0;
  }

  .two {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 10px;
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

  /* the hint of an option is shown only for the chosen one, so the column stays short */
  .opt:not(.on) small {
    display: none;
  }

  .grow {
    flex: 1 1 200px;
    min-width: 0;
  }

  .cmd {
    margin: 0;
    padding: 8px 10px;
    overflow-x: auto;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    border: 2px dashed var(--seg-off);
    background: var(--bg);
    color: var(--muted);
    font-size: 12px;
  }

  @media (max-width: 760px) {
    .cols,
    .two {
      grid-template-columns: 1fr;
    }
  }
</style>
