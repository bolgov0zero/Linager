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
  const size = $derived(small ? 13.5 : 18);
  // a square button: a window header's buttons have 2 px above and below, those of a tab 5 px
  const pad = $derived(small ? 'padding:2px 5px' : 'padding:5px 8px');
</script>

{#if canFiles}
  <button class="btn sc" class:sm={small} style={pad} type="button" title={$tr('shortcut.files')} aria-label={$tr('shortcut.files')} onclick={() => openFilesWindow(serverId, serverName)}><span class="strut" aria-hidden="true">&#8203;</span><PixIcon name="folder" {size} /></button>
{/if}
{#if canDispatcher}
  <button class="btn sc" class:sm={small} style={pad} type="button" title={$tr('disp.button')} aria-label={$tr('disp.button')} onclick={() => openDispatcherWindow(serverId, serverName)}><span class="strut" aria-hidden="true">&#8203;</span><PixIcon name="tasks" {size} /></button>
{/if}

<style>
  /*
   * The height of the neighbours' text buttons: the invisible character below sits on a line of the button's own font, so the
   * button is as tall as a button with words. The padding is set inline: the header of a window sets its own for .btn.sm.
   */
  .sc {
    display: inline-flex;
    align-items: center;
    justify-content: center;
  }

  .strut {
    display: block;
    width: 0;
    overflow: hidden;
  }
</style>
