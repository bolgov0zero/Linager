<script lang="ts">
  import { tr } from '../../i18n';
  import WindowFrame from '../terminal/WindowFrame.svelte';
  import { type TermWindow } from '../terminal/windows';
  import DesktopSession, { type DeskPhase } from './DesktopSession.svelte';
  import DesktopTools from './DesktopTools.svelte';

  let { win, top }: { win: TermWindow; top: boolean } = $props();

  let phase = $state<DeskPhase>('form');
  let autoResize = $state(true);
  let session = $state<DesktopSession | null>(null);

  // The server never changes for a window; deriving keeps the session from restarting when the store replaces the object.
  const serverId = $derived(win.serverId);
  const address = $derived(win.address ?? '');

  const statusText = $derived(
    phase === 'connecting' ? $tr('desk.connecting') : phase === 'open' ? $tr('desk.open') : $tr('desk.closed'),
  );
</script>

<WindowFrame {win} {top} ariaLabel={$tr('deskwin.title', { name: win.name })} onrefocus={() => session?.focus()}>
  {#snippet title()}
    <span class="dot" class:on={phase === 'open'} aria-hidden="true"></span>
    <span class="label">{win.name}</span>
    <span class="muted status">· {statusText}</span>
  {/snippet}
  {#snippet tools()}
    <DesktopTools {session} {phase} {serverId} serverName={win.serverName ?? win.name} bind:autoResize />
  {/snippet}
  <DesktopSession {serverId} {address} bind:phase bind:autoResize bind:this={session} />
</WindowFrame>
