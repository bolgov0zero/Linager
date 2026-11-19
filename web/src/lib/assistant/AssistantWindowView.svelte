<script lang="ts">
  import { tr } from '../../i18n';
  import WindowFrame from '../terminal/WindowFrame.svelte';
  import { updateTermWindow, type TermWindow } from '../terminal/windows';
  import AssistantBadge from './AssistantBadge.svelte';
  import AssistantSession, { type AssistInfo, type AssistPhase } from './AssistantSession.svelte';
  import AssistantStats from './AssistantStats.svelte';
  import AssistantTools from './AssistantTools.svelte';
  import { gotoServerTab } from './nav';

  let { win, top }: { win: TermWindow; top: boolean } = $props();

  let phase = $state<AssistPhase>('connecting');
  let info = $state<AssistInfo | null>(null);
  let screen = $state<AssistantSession | null>(null);
  // The server never changes for a window; deriving keeps the session from restarting when the store replaces the object.
  const serverId = $derived(win.serverId);
  const serverName = $derived(win.serverName ?? win.name);
  // The session lives in the window record, so that the store can tell which sessions are open already.
  const session = $derived(win.session ?? 'console');

  const statusText = $derived(
    phase === 'connecting' ? $tr('assist.connectingShort') : phase === 'open' ? $tr('assist.open') : $tr('assist.closed'),
  );
</script>

<WindowFrame {win} {top} ariaLabel={$tr('assistwin.title', { name: serverName })} onrefocus={() => screen?.focus()}>
  {#snippet title()}
    <span class="dot" class:on={phase === 'open'} aria-hidden="true"></span>
    <span class="label">{serverName}</span>
    <span class="muted status">· {$tr('assist.name')} · {statusText}{#if phase === 'open'} <AssistantBadge desktop={info?.desktop} />{/if}</span>
  {/snippet}
  {#snippet tools()}
    <AssistantTools {serverId} bind:session={() => session, (v) => updateTermWindow(win.key, { session: v })} {phase} {info} {screen} onsettings={() => gotoServerTab(serverName, 'manage')} />
  {/snippet}
  <div class="col">
    <div class="screenbox"><AssistantSession {serverId} {session} bind:phase bind:info bind:this={screen} /></div>
    <AssistantStats {serverId} {phase} {info} />
  </div>
</WindowFrame>

<style>
  .col {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
    border: 2px solid var(--line);
  }

  .screenbox {
    flex: 1;
    min-height: 0;
    display: flex;
  }
</style>
