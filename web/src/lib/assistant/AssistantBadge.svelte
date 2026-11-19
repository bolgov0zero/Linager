<script lang="ts">
  import { tr } from '../../i18n';
  import { isSecureDesktop } from './protocol';

  /** The warning that the computer shows the lock, sign-in or UAC screen (what is seen is not the desktop of the user). Nothing when the desktop is the usual one. */
  let { desktop }: { desktop?: string | null } = $props();
</script>

{#if isSecureDesktop(desktop)}
  <span class="badge" role="status">{$tr('assist.secureDesktop')}</span>
{/if}

<style>
  .badge {
    display: inline-block;
    padding: 0 5px;
    border: 2px solid var(--warn);
    color: var(--warn);
    font-size: 11px;
    line-height: 1.35;
    white-space: nowrap;
  }
</style>
