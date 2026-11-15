<script lang="ts">
  import type { Session } from '../../api';
  import { tr } from '../../i18n';
  import Modal from '../Modal.svelte';
  import AuditPage from '../AuditPage.svelte';
  import UpdatesPage from '../UpdatesPage.svelte';
  import ProfileSection from './ProfileSection.svelte';
  import SnippetsSection from './SnippetsSection.svelte';
  import UsersSection from './UsersSection.svelte';

  import { SETTINGS_SECTIONS, type SettingsSection } from './sections';

  let {
    section,
    session,
    updatesCount,
    onsection,
    onclose,
    onauthlost,
    onrefresh,
    onupdateschanged,
  }: {
    section: SettingsSection;
    session: Session;
    updatesCount: number;
    onsection: (s: SettingsSection) => void;
    onclose: () => void;
    onauthlost: () => void;
    onrefresh: () => Promise<void>;
    onupdateschanged: () => void;
  } = $props();

  /** An operator and an auditor only have their profile. */
  const isAdmin = $derived((session.role ?? 'admin') === 'admin');
</script>

<Modal title={isAdmin ? $tr('settings.title') : $tr('app.profile')} labelId="settings-title" {onclose} wide={isAdmin}>
  <div class="settings" class:single={!isAdmin}>
    {#if isAdmin}
    <nav class="side" aria-label={$tr('settings.nav')}>
      {#each SETTINGS_SECTIONS as s (s)}
        <button class="nav" class:active={section === s} type="button" aria-current={section === s ? 'page' : undefined} onclick={() => onsection(s)}>
          {$tr(`settings.section.${s}`)}
          {#if s === 'updates' && updatesCount > 0}<span class="badge">{updatesCount}</span>{/if}
        </button>
      {/each}
    </nav>
    {/if}
    <div class="content">
      {#if section === 'profile'}
        <ProfileSection {session} {onrefresh} {onauthlost} />
      {:else if section === 'users'}
        <UsersSection {onauthlost} />
      {:else if section === 'updates'}
        <UpdatesPage embedded onchanged={onupdateschanged} />
      {:else if section === 'audit'}
        <AuditPage embedded />
      {:else}
        <SnippetsSection {onauthlost} />
      {/if}
    </div>
  </div>
</Modal>

<style>
  .settings {
    display: grid;
    grid-template-columns: 190px minmax(0, 1fr);
    gap: 20px;
    min-height: min(640px, calc(100dvh - 180px));
  }

  .settings.single {
    display: block;
    min-height: 0;
  }

  .side {
    display: flex;
    flex-direction: column;
    gap: 6px;
    align-self: start;
  }

  .nav {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    padding: 8px 12px;
    text-align: left;
    background: var(--surface);
    color: var(--ink);
    border: 2px solid var(--line);
    border-radius: 0;
    font-family: var(--font-display);
    font-size: 14px;
    cursor: pointer;
    box-shadow: 3px 3px 0 var(--shade);
  }

  .nav.active {
    background: var(--ink);
    color: var(--surface);
  }

  .badge {
    padding: 0 6px;
    border: 2px solid currentColor;
    font-size: 12px;
  }

  .content {
    min-width: 0;
    max-height: calc(100dvh - 190px);
    overflow-y: auto;
    padding: 2px 4px 8px 2px;
  }

  @media (max-width: 720px) {
    .settings {
      grid-template-columns: minmax(0, 1fr);
      grid-auto-rows: max-content;
      gap: 12px;
      min-height: 0;
    }

    .side {
      flex-direction: row;
      overflow-x: auto;
      padding-bottom: 6px;
    }

    .nav {
      flex: none;
    }

    .content {
      max-height: none;
    }
  }
</style>
