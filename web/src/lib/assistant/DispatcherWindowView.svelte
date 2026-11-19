<script lang="ts">
  import { api } from '../../api';
  import { tr } from '../../i18n';
  import { osKind } from '../dashboard/status';
  import WindowFrame from '../terminal/WindowFrame.svelte';
  import type { TermWindow } from '../terminal/windows';
  import { dispatcherTabs, myAccess } from './access';
  import DispatcherProcesses from './DispatcherProcesses.svelte';
  import DispatcherServices from './DispatcherServices.svelte';
  import './dispatcher.css';

  /** The Dispatcher of the Assistant: the processes and the services of the computer in a floating window, as the Task Manager has them. */
  let { win, top }: { win: TermWindow; top: boolean } = $props();

  // The window gets a new object on every move; the server never changes for it.
  const serverId = $derived(win.serverId);
  const tabs = $derived(dispatcherTabs($myAccess.role, $myAccess.modules));

  // The computer decides what "end" means (a signal on Linux, the process or its tree on Windows): its system is read once.
  let os = $state('');
  $effect(() => {
    const id = serverId;
    let alive = true;
    void api
      .server(id)
      .then((d) => {
        if (alive) os = d.os;
      })
      .catch(() => undefined);
    return () => {
      alive = false;
    };
  });
  const windows = $derived(osKind(os) === 'windows');

  let picked = $state<'proc' | 'svc'>('proc');
  // a tab the person has no module for is not offered, the other one is shown instead
  const tab = $derived(picked === 'proc' ? (tabs.proc || !tabs.svc ? 'proc' : 'svc') : tabs.svc || !tabs.proc ? 'svc' : 'proc');
  let query = $state('');
  const active = $derived(!win.minimized);

  function go(next: 'proc' | 'svc'): void {
    picked = next;
    query = '';
  }
</script>

<WindowFrame {win} {top} ariaLabel={$tr('disp.windowTitle', { name: win.name })}>
  {#snippet title()}
    <span class="label">{win.name}</span>
    <span class="muted status">· {$tr('disp.title')}</span>
  {/snippet}
  <div class="dsp">
    <div class="dsp-head">
      {#if tabs.proc}<button class="chip" type="button" aria-pressed={tab === 'proc'} onclick={() => go('proc')}>{$tr('disp.tabProc')}</button>{/if}
      {#if tabs.svc}<button class="chip" type="button" aria-pressed={tab === 'svc'} onclick={() => go('svc')}>{$tr('disp.tabSvc')}</button>{/if}
      <span class="sp"></span>
      <input class="input" type="search" placeholder={tab === 'proc' ? $tr('disp.searchProc') : $tr('disp.searchSvc')} aria-label={$tr('disp.search')} bind:value={query} />
    </div>
    {#if tab === 'proc'}
      <DispatcherProcesses {serverId} {query} {active} {windows} />
    {:else}
      <DispatcherServices {serverId} {query} {active} />
    {/if}
  </div>
</WindowFrame>
