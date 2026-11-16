<script lang="ts">
  import { api, type Group, type Role, type Server, type UserAccess } from '../../api';
  import { tr } from '../../i18n';
  import Select from '../ui/Select.svelte';
  import AccessModulesModal from './AccessModulesModal.svelte';
  import AccessServersModal from './AccessServersModal.svelte';

  let {
    role = $bindable(),
    access = $bindable(),
    allowAdmin = true,
  }: {
    role: Role;
    access: UserAccess;
    allowAdmin?: boolean;
  } = $props();

  let serversOpen = $state(false);
  let modulesOpen = $state(false);
  let servers = $state<Server[]>([]);
  let groups = $state<Group[]>([]);
  let loadError = $state(false);

  const roles = $derived([
    ...(allowAdmin ? [{ value: 'admin' as Role, label: $tr('users.role.admin') }] : []),
    { value: 'operator' as Role, label: $tr('users.role.operator') },
    { value: 'auditor' as Role, label: $tr('users.role.auditor') },
  ]);

  const serverCount = $derived(
    new Set([...access.servers, ...servers.filter((s) => s.group_id != null && access.groups.includes(s.group_id)).map((s) => s.id)]).size ||
      access.servers.length,
  );

  async function openServers(): Promise<void> {
    loadError = false;
    try {
      [servers, groups] = await Promise.all([api.servers(), api.groups()]);
      serversOpen = true;
    } catch {
      loadError = true;
    }
  }
</script>

<div class="editor stack">
  <label class="field">
    <span class="label">{$tr('users.role')}</span>
    <Select bind:value={role} ariaLabel={$tr('users.role')} options={roles} />
  </label>
  <p class="muted small">{$tr(`users.roleHint.${role}`)}</p>
  {#if role !== 'admin'}
    <div class="row">
      <button class="btn" type="button" onclick={() => void openServers()}>
        {$tr('acc.servers.button')} <span class="count">{serverCount}</span>
      </button>
      {#if role === 'operator'}
        <button class="btn" type="button" onclick={() => (modulesOpen = true)}>
          {$tr('acc.modules.button')} <span class="count">{access.modules.length}</span>
        </button>
      {/if}
    </div>
    {#if loadError}<p class="error small" role="alert">{$tr('common.genericError')}</p>{/if}
  {/if}
</div>

{#if serversOpen}
  <AccessServersModal
    {servers}
    {groups}
    selectedServers={access.servers}
    selectedGroups={access.groups}
    onsave={(s, g) => {
      access = { ...access, servers: s, groups: g };
      serversOpen = false;
    }}
    onclose={() => (serversOpen = false)}
  />
{/if}

{#if modulesOpen}
  <AccessModulesModal
    selected={access.modules}
    onsave={(m) => {
      access = { ...access, modules: m };
      modulesOpen = false;
    }}
    onclose={() => (modulesOpen = false)}
  />
{/if}

<style>
  .editor {
    min-width: 0;
  }

  p {
    margin: 0;
  }

  .count {
    margin-left: 4px;
    color: var(--muted);
  }

  .error {
    color: var(--crit);
  }

  .row :global(.btn) {
    display: inline-flex;
    align-items: center;
    height: 36px;
    padding-block: 0;
  }

  @media (max-width: 720px) {
    .row {
      flex-direction: column;
      align-items: stretch;
    }

    .row :global(.btn) {
      height: auto;
      min-height: 44px;
    }
  }
</style>
