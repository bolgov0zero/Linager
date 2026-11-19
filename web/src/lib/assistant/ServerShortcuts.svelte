<script lang="ts">
  import { tr } from '../../i18n';
  import PixIcon from '../ui/PixIcon.svelte';
  import { openDispatcherWindow, openFilesWindow } from '../terminal/windows';
  import { dispatcherTabs, myAccess } from './access';

  /**
   * The two icon buttons every connection (terminal, remote desktop, Assistant) has on its bar: the files of the computer and the
   * Dispatcher (processes and services), each in a floating window. A button is there for whoever has the right for it.
   */
  let { serverId, serverName, small = false }: { serverId: string; serverName: string; /** The buttons of a floating window's header are smaller. */ small?: boolean } = $props();

  const can = (module: string): boolean => $myAccess.role === 'admin' || ($myAccess.role === 'operator' && $myAccess.modules.includes(module));
  const canFiles = $derived(can('files'));
  const tabs = $derived(dispatcherTabs($myAccess.role, $myAccess.modules));
  const canDispatcher = $derived(tabs.proc || tabs.svc);
  const size = $derived(small ? 16 : 24);
</script>

{#if canFiles}
  <button class="btn icon" class:sm={small} type="button" title={$tr('shortcut.files')} aria-label={$tr('shortcut.files')} onclick={() => openFilesWindow(serverId, serverName)}><PixIcon name="folder" {size} /></button>
{/if}
{#if canDispatcher}
  <button class="btn icon" class:sm={small} type="button" title={$tr('disp.button')} aria-label={$tr('disp.button')} onclick={() => openDispatcherWindow(serverId, serverName)}><PixIcon name="tasks" {size} /></button>
{/if}

