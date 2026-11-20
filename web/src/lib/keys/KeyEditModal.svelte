<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import { tr } from '../../i18n';
  import Modal from '../Modal.svelte';
  import AccessServersModal from '../settings/AccessServersModal.svelte';
  import { loadDirectory, putEntry, type Directory } from './store';
  import { TYPES, bindText } from './labels';
  import { LIMITS, blankEntry, type Entry, type EntryType, type Ref } from './vault';

  /** Adds a key (`entry` is null) or changes one. The change goes into the open vault in memory; the file is saved from the window. */
  let { entry, onclose }: { entry: Entry | null; onclose: () => void } = $props();

  const start = untrack(() => entry ?? blankEntry());
  let name = $state(start.name);
  let type = $state<EntryType>(start.type);
  let groups = $state<Ref[]>(start.groups.map((g) => ({ ...g })));
  let servers = $state<Ref[]>(start.servers.map((s) => ({ ...s })));
  let login = $state(start.login);
  let domain = $state(start.domain);
  let password = $state(start.password);
  let note = $state(start.note);
  let shown = $state(false);
  let error = $state('');

  let dir = $state<Directory | null>(null);
  let dirFailed = $state(false);
  let picking = $state(false);

  onMount(() => {
    loadDirectory().then(
      (d) => (dir = d),
      () => (dirFailed = true),
    );
  });

  // What the panel does not know any more stays in the key until it is taken off here.
  const goneGroups = $derived(dir ? groups.filter((g) => !dir!.groups.some((x) => String(x.id) === g.id)) : []);
  const goneServers = $derived(dir ? servers.filter((s) => !dir!.servers.some((x) => x.id === s.id)) : []);

  function applyBinding(pickedServers: string[], pickedGroups: number[]): void {
    if (!dir) return;
    const d = dir;
    const groupSet = new Set(pickedGroups);
    const keepG = groups.filter((g) => !d.groups.some((x) => String(x.id) === g.id));
    const keepS = servers.filter((s) => !d.servers.some((x) => x.id === s.id));
    groups = [
      ...pickedGroups.map((id) => ({ id: String(id), name: d.groups.find((g) => g.id === id)?.name ?? String(id) })),
      ...keepG,
    ];
    // a server of a chosen group is covered by the group
    servers = [
      ...pickedServers
        .filter((id) => {
          const gid = d.servers.find((s) => s.id === id)?.group_id;
          return gid == null || !groupSet.has(gid);
        })
        .map((id) => ({ id, name: d.servers.find((s) => s.id === id)?.name ?? id })),
      ...keepS,
    ];
    picking = false;
  }

  function forget(kind: 'g' | 's', id: string): void {
    if (kind === 'g') groups = groups.filter((g) => g.id !== id);
    else servers = servers.filter((s) => s.id !== id);
  }

  const ALPHABET = 'abcdefghjkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789';
  function generate(): void {
    const bytes = new Uint8Array(20);
    crypto.getRandomValues(bytes);
    password = Array.from(bytes, (b) => ALPHABET[b % ALPHABET.length]).join('');
    shown = true;
  }

  function submit(event: SubmitEvent): void {
    event.preventDefault();
    if (!name.trim() || !login.trim()) {
      error = $tr('keys.edit.need');
      return;
    }
    const ok = putEntry({ id: start.id, name: name.trim(), type, groups, servers, login: login.trim(), domain: domain.trim(), password, note: note.trim() });
    if (!ok) {
      error = $tr('keys.err.limit');
      return;
    }
    onclose();
  }
</script>

<Modal
  title={entry ? $tr('keys.edit.title', { name: entry.name }) : $tr('keys.edit.new')}
  labelId="key-edit-title"
  {onclose}
  onescape={() => {
    if (!picking) onclose(); // Escape closes the picker on top first
  }}>
  <form class="stack" onsubmit={submit} autocomplete="off">
    <label class="field">
      <span class="label">{$tr('keys.f.name')}</span>
      <input class="input" bind:value={name} maxlength={LIMITS.name} placeholder={$tr('keys.f.name.ph')} autocomplete="off" />
    </label>

    <div class="field">
      <span class="label">{$tr('keys.f.use')}</span>
      <div class="row types">
        {#each TYPES as ty (ty)}
          <button class="chip" type="button" aria-pressed={type === ty} onclick={() => (type = ty)}>{$tr(`keys.type.${ty}`)}</button>
        {/each}
      </div>
      <span class="small muted">{$tr(`keys.use.${type}`)}</span>
    </div>

    <div class="field">
      <span class="label">{$tr('keys.f.bind')}</span>
      <button class="bind" type="button" disabled={!dir} onclick={() => (picking = true)}>
        <span class="text">{bindText({ groups, servers }, $tr)}</span>
        <span class="small accent">{$tr('keys.f.bind.change')}</span>
      </button>
      {#if dirFailed}<span class="small error">{$tr('keys.dirError')}</span>{/if}
      {#if goneGroups.length + goneServers.length > 0}
        <div class="row gone">
          <span class="small muted">{$tr('keys.gone.label')}</span>
          {#each goneGroups as g (g.id)}
            <button class="chip" type="button" title={$tr('keys.gone.remove', { name: g.name })} onclick={() => forget('g', g.id)}>{$tr('keys.bind.group', { name: g.name })} ×</button>
          {/each}
          {#each goneServers as s (s.id)}
            <button class="chip" type="button" title={$tr('keys.gone.remove', { name: s.name })} onclick={() => forget('s', s.id)}>{s.name} ×</button>
          {/each}
        </div>
      {/if}
    </div>

    <div class="two">
      <label class="field">
        <span class="label">{$tr('keys.f.login')}</span>
        <input class="input" bind:value={login} maxlength={LIMITS.login} autocomplete="off" spellcheck="false" autocapitalize="off" />
      </label>
      <label class="field">
        <span class="label">{$tr('keys.f.domain')}</span>
        <input class="input" bind:value={domain} maxlength={LIMITS.domain} autocomplete="off" spellcheck="false" autocapitalize="off" />
      </label>
    </div>

    <div class="field">
      <label class="label" for="key-pass">{$tr('keys.f.pass')}</label>
      <div class="row pw">
        <input id="key-pass" class="input" type={shown ? 'text' : 'password'} bind:value={password} maxlength={LIMITS.password} autocomplete="new-password" spellcheck="false" />
        <button class="btn" type="button" aria-pressed={shown} onclick={() => (shown = !shown)}>{shown ? $tr('keys.pass.hide') : $tr('keys.pass.show')}</button>
        <button class="btn" type="button" onclick={generate}>{$tr('keys.pass.gen')}</button>
      </div>
    </div>

    <label class="field">
      <span class="label">{$tr('keys.f.note')}</span>
      <textarea class="input" rows="2" bind:value={note} maxlength={LIMITS.note}></textarea>
    </label>

    <p class="small muted">{$tr('keys.edit.memo')}</p>
    {#if error}<p class="error" role="alert">{error}</p>{/if}
    <div class="row end">
      <button class="btn" type="button" onclick={onclose}>{$tr('common.cancel')}</button>
      <button class="btn primary" type="submit">{$tr('keys.edit.done')}</button>
    </div>
  </form>
</Modal>

{#if picking && dir}
  <AccessServersModal
    servers={dir.servers}
    groups={dir.groups}
    selectedServers={servers.map((s) => s.id).filter((id) => dir!.servers.some((x) => x.id === id))}
    selectedGroups={groups.map((g) => Number(g.id)).filter((id) => dir!.groups.some((x) => x.id === id))}
    title={$tr('keys.bindModal.title')}
    hint={$tr('keys.bindModal.hint')}
    totalKey="keys.bindModal.total"
    zeroKey="keys.bindModal.none"
    onsave={applyBinding}
    onclose={() => (picking = false)} />
{/if}

<style>
  .types {
    gap: 0;
  }

  .types .chip + .chip {
    margin-left: -2px;
  }

  .two {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 9px;
  }

  .pw :global(.input) {
    flex: 1;
    min-width: 0;
  }

  .bind {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 9px;
    width: 100%;
    min-height: 36px;
    padding: 5px 9px;
    background: var(--surface);
    color: var(--ink);
    border: 2px solid var(--line);
    font: inherit;
    text-align: left;
    cursor: pointer;
  }

  .bind:disabled {
    opacity: 0.5;
    cursor: wait;
  }

  .bind .text {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .accent {
    color: var(--accent);
    white-space: nowrap;
  }

  .gone {
    gap: 6px;
  }

  p {
    margin: 0;
  }

  @media (max-width: 720px) {
    .two {
      grid-template-columns: 1fr;
    }
  }
</style>
