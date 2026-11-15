<script lang="ts">
  import { get } from 'svelte/store';
  import { onMount } from 'svelte';
  import { api, isUnauthorized, type FileStat } from '../../../api';
  import { locale, t, tr as tStore, type Vars } from '../../../i18n';
  import Modal from '../../Modal.svelte';
  import { showToast } from '../../toast';
  import { formatBytes, formatDateTime } from '../format';
  import { errorText } from './files-util';

  let {
    serverId,
    path,
    offline,
    onclose,
    onchanged,
    onauthlost,
  }: {
    serverId: string;
    /** Full path of the entry. */
    path: string;
    offline: boolean;
    onclose: () => void;
    /** Permissions or owner were changed: the listing should be refreshed. */
    onchanged: () => void;
    onauthlost: () => void;
  } = $props();

  function T(key: string, vars?: Vars): string {
    return t(key, get(locale), vars);
  }

  let stat = $state<FileStat | null>(null);
  let loadFailed = $state(false);
  let du = $state<{ size: number; files: number; dirs: number; truncated: boolean } | null>(null);
  let duBusy = $state(false);

  /** Nine rwx bits (owner, group, others) and the special bits, kept together with their octal text. */
  let bits = $state<boolean[]>(Array(9).fill(false));
  let special = $state<boolean[]>([false, false, false]); // setuid, setgid, sticky
  let octal = $state('0000');
  let owner = $state('');
  let group = $state('');
  let recursive = $state(false);
  let confirm = $state<'perms' | 'owner' | null>(null);
  let busy = $state(false);
  let error = $state('');

  const isDir = $derived(stat?.type === 'dir');
  const locked = $derived(offline || (stat?.protected ?? false));
  const modeValue = $derived((special[0] ? 0o4000 : 0) | (special[1] ? 0o2000 : 0) | (special[2] ? 0o1000 : 0) | bits.reduce((n, on, i) => n | (on ? 1 << (8 - i) : 0), 0));

  function loadMode(mode: string): void {
    const v = parseInt(mode, 8) || 0;
    bits = Array.from({ length: 9 }, (_, i) => (v & (1 << (8 - i))) !== 0);
    special = [(v & 0o4000) !== 0, (v & 0o2000) !== 0, (v & 0o1000) !== 0];
    octal = (v & 0o7777).toString(8).padStart(4, '0');
  }

  function toggleBit(i: number): void {
    bits[i] = !bits[i];
    octal = modeValue.toString(8).padStart(4, '0');
  }

  function toggleSpecial(i: number): void {
    special[i] = !special[i];
    octal = modeValue.toString(8).padStart(4, '0');
  }

  function onOctal(value: string): void {
    octal = value.replace(/[^0-7]/g, '').slice(0, 4);
    if (octal.length >= 3) loadModeKeep(octal);
  }

  /** Updates the checkboxes from typed digits without rewriting what the user is typing. */
  function loadModeKeep(text: string): void {
    const v = parseInt(text, 8) || 0;
    bits = Array.from({ length: 9 }, (_, i) => (v & (1 << (8 - i))) !== 0);
    special = [(v & 0o4000) !== 0, (v & 0o2000) !== 0, (v & 0o1000) !== 0];
  }

  async function load(): Promise<void> {
    try {
      const s = await api.fileStat(serverId, path);
      stat = s;
      loadMode(s.mode);
      owner = s.owner;
      group = s.group;
    } catch (e) {
      if (isUnauthorized(e)) onauthlost();
      else loadFailed = true;
    }
  }

  onMount(() => void load());

  async function calc(): Promise<void> {
    duBusy = true;
    try {
      du = await api.fileDu(serverId, path);
    } catch (e) {
      if (isUnauthorized(e)) onauthlost();
      else error = errorText(e, T);
    } finally {
      duBusy = false;
    }
  }

  const needsConfirm = $derived(recursive || special.some(Boolean));

  function askPerms(): void {
    error = '';
    if (needsConfirm) confirm = 'perms';
    else void applyPerms(false);
  }

  async function applyPerms(confirmed: boolean): Promise<void> {
    confirm = null;
    busy = true;
    error = '';
    try {
      const mode = (modeValue & 0o7777).toString(8).padStart(4, '0');
      await api.fileChmod(serverId, path, mode, recursive && isDir, confirmed);
      showToast(T('files.props.permsDone'));
      onchanged();
      await load();
    } catch (e) {
      if (isUnauthorized(e)) onauthlost();
      else error = errorText(e, T);
    } finally {
      busy = false;
    }
  }

  async function applyOwner(): Promise<void> {
    confirm = null;
    busy = true;
    error = '';
    try {
      await api.fileChown(serverId, path, owner.trim(), group.trim(), recursive && isDir);
      showToast(T('files.props.ownerDone'));
      onchanged();
      await load();
    } catch (e) {
      if (isUnauthorized(e)) onauthlost();
      else error = errorText(e, T);
    } finally {
      busy = false;
    }
  }

  const extra = $derived(
    (recursive && isDir ? T('files.props.extraRec') : '') + (special.some(Boolean) && confirm === 'perms' ? T('files.props.extraSpecial') : ''),
  );
  const when = (ts: number) => (ts > 0 ? formatDateTime(ts, $locale) : T('files.props.unknown'));
  const ROWS = ['files.props.owner', 'files.props.group', 'files.props.other'] as const;
  const COLS = ['files.props.read', 'files.props.write', 'files.props.exec'] as const;
</script>

<Modal title={`${$tStore('files.props.title')}: ${stat?.name ?? path}`} labelId="files-props-title" {onclose}>
  {#if loadFailed}
    <p class="error" role="alert">{$tStore('files.props.loadFailed')}</p>
  {:else if !stat}
    <p class="muted">{$tStore('common.loading')}</p>
  {:else}
    <dl class="kv">
      <dt>{$tStore('files.props.path')}</dt>
      <dd class="mono">{stat.path}</dd>
      <dt>{$tStore('files.props.type')}</dt>
      <dd>{$tStore(`files.kind.${stat.type}`)}{stat.mime ? ` · ${stat.mime}` : ''}</dd>
      {#if stat.link_target}
        <dt>{$tStore('files.props.target')}</dt>
        <dd class="mono">{stat.link_target}</dd>
      {/if}
      <dt>{$tStore('files.props.size')}</dt>
      <dd class="mono">{stat.type === 'dir' ? '—' : `${formatBytes(stat.size)} (${stat.size} B)`}</dd>
      {#if isDir}
        <dt>{$tStore('files.props.content')}</dt>
        <dd>
          {#if du}
            <span class="mono">{$tStore(du.truncated ? 'files.props.calcPartial' : 'files.props.calcResult', { size: formatBytes(du.size), files: du.files, dirs: du.dirs })}</span>
          {:else}
            <button class="btn sm" type="button" disabled={duBusy || offline} onclick={calc}>{duBusy ? $tStore('common.loading') : $tStore('files.props.calc')}</button>
          {/if}
        </dd>
      {/if}
      <dt>{$tStore('files.props.created')}</dt>
      <dd class="mono">{when(stat.btime)}</dd>
      <dt>{$tStore('files.props.modified')}</dt>
      <dd class="mono">{when(stat.mtime)}</dd>
      <dt>{$tStore('files.props.changed')}</dt>
      <dd class="mono">{when(stat.ctime)}</dd>
      <dt>{$tStore('files.props.accessed')}</dt>
      <dd class="mono">{when(stat.atime)}</dd>
      <dt>{$tStore('files.props.links')} / {$tStore('files.props.inode')}</dt>
      <dd class="mono">{stat.links} / {stat.inode}</dd>
    </dl>

    <section class="block">
      <h3>{$tStore('files.props.perms')}</h3>
      {#if offline}
        <p class="muted small">{$tStore('files.props.offline')}</p>
      {:else if stat.protected}
        <p class="muted small">{$tStore('files.props.system')}</p>
      {:else if stat.type === 'link'}
        <p class="muted small">{$tStore('files.props.linkNote')}</p>
      {/if}
      <table class="perm">
        <thead>
          <tr>
            <th></th>
            {#each COLS as c (c)}<th>{$tStore(c)}</th>{/each}
          </tr>
        </thead>
        <tbody>
          {#each ROWS as r, ri (r)}
            <tr>
              <th scope="row">{$tStore(r)}</th>
              {#each COLS as c, ci (c)}
                <td>
                  <input
                    type="checkbox"
                    aria-label="{$tStore(r)}: {$tStore(c)}"
                    checked={bits[ri * 3 + ci]}
                    disabled={locked || stat.type === 'link'}
                    onchange={() => toggleBit(ri * 3 + ci)} />
                </td>
              {/each}
            </tr>
          {/each}
        </tbody>
      </table>
      <div class="row">
        {#each ['files.props.suid', 'files.props.sgid', 'files.props.sticky'] as k, i (k)}
          <label class="check">
            <input type="checkbox" checked={special[i]} disabled={locked || stat.type === 'link'} onchange={() => toggleSpecial(i)} />
            <span class="mono small">{$tStore(k)}</span>
          </label>
        {/each}
        <label class="row grow end">
          <span class="muted small">{$tStore('files.props.octal')}</span>
          <input
            class="input mono octal"
            maxlength="4"
            inputmode="numeric"
            value={octal}
            disabled={locked || stat.type === 'link'}
            oninput={(e) => onOctal(e.currentTarget.value)} />
        </label>
      </div>
      <div class="row end">
        <button class="btn primary" type="button" disabled={busy || locked || stat.type === 'link' || octal.length < 3} onclick={askPerms}>
          {$tStore('files.props.applyPerms')}
        </button>
      </div>
    </section>

    <section class="block">
      <h3>{$tStore('files.props.owner')} / {$tStore('files.props.group')}</h3>
      <div class="row">
        <label class="field grow">
          <span class="label">{$tStore('files.props.owner')}</span>
          <input class="input mono" bind:value={owner} disabled={locked} maxlength="64" autocomplete="off" spellcheck="false" />
        </label>
        <label class="field grow">
          <span class="label">{$tStore('files.props.group')}</span>
          <input class="input mono" bind:value={group} disabled={locked} maxlength="64" autocomplete="off" spellcheck="false" />
        </label>
      </div>
      <div class="row end">
        <button class="btn" type="button" disabled={busy || locked || (!owner.trim() && !group.trim())} onclick={() => (confirm = 'owner')}>
          {$tStore('files.props.applyOwner')}
        </button>
      </div>
    </section>

    {#if isDir && !locked}
      <label class="check">
        <input type="checkbox" bind:checked={recursive} />
        <span>{$tStore('files.props.recursive')}</span>
      </label>
    {/if}

    {#if confirm}
      <div class="strip" role="alert">
        <p>{$tStore(confirm === 'perms' ? 'files.props.confirmPerms' : 'files.props.confirmOwner', { extra: confirm === 'owner' ? (recursive && isDir ? T('files.props.extraRec') : '') : extra })}</p>
        <div class="row end">
          <button class="btn" type="button" onclick={() => (confirm = null)}>{$tStore('files.props.cancel')}</button>
          <button class="btn danger" type="button" disabled={busy} onclick={() => (confirm === 'perms' ? void applyPerms(true) : void applyOwner())}>
            {$tStore('files.props.confirm')}
          </button>
        </div>
      </div>
    {/if}

    {#if error}
      <p class="error" role="alert">{error}</p>
    {/if}
  {/if}
  <div class="row end">
    <button class="btn" type="button" onclick={onclose}>{$tStore('files.props.close')}</button>
  </div>
</Modal>

<style>
  .kv {
    display: grid;
    grid-template-columns: max-content minmax(0, 1fr);
    gap: 6px 16px;
    margin: 0;
  }

  .kv dt {
    color: var(--muted);
  }

  .kv dd {
    margin: 0;
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .block {
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding-top: 12px;
    border-top: 2px dashed var(--seg-off);
  }

  h3 {
    margin: 0;
    font-size: 15px;
    color: var(--muted);
    text-transform: uppercase;
    letter-spacing: 0.06em;
  }

  .perm {
    border-collapse: collapse;
  }

  .perm th {
    padding: 4px 12px 4px 0;
    text-align: left;
    font-weight: 400;
    color: var(--muted);
    font-size: 12px;
  }

  .perm td {
    padding: 4px 16px 4px 0;
  }

  .grow {
    flex: 1 1 140px;
    min-width: 0;
  }

  .octal {
    width: 5.5em;
  }

  .strip {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 10px 12px;
    border: 2px solid var(--warn);
  }

  .strip p {
    margin: 0;
  }
</style>
