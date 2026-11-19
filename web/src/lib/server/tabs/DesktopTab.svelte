<script lang="ts">
  import type { ServerDetail } from '../../../api';
  import { tr } from '../../../i18n';
  import DesktopSession, { type DeskPhase } from '../../desktop/DesktopSession.svelte';
  import DesktopTools from '../../desktop/DesktopTools.svelte';
  import { openDesktopWindow } from '../../terminal/windows';

  /** docked: the tab fills the Console (lib/pult): it takes the height of its parent and has no "in a window" button. */
  let { server, docked = false }: { server: ServerDetail; docked?: boolean } = $props();

  let phase = $state<DeskPhase>('form');
  let autoResize = $state(true);
  let session = $state<DesktopSession | null>(null);

  // Derived primitives: the parent replaces `server` on every live metrics event, which must not restart the session.
  const serverId = $derived(server.id);
  const address = $derived(server.address);

  const statusText = $derived(
    phase === 'connecting' ? $tr('desk.connecting') : phase === 'open' ? $tr('desk.open') : $tr('desk.closed'),
  );

  /** Moves the desktop into a floating window: this tab's own connection ends first (one desktop per computer). */
  function popOut(): void {
    session?.disconnect();
    setTimeout(() => openDesktopWindow(serverId, server.name, address), 150);
  }
</script>

<div class="deskTab" class:docked>
  <div class="head">
    <p class="meta">
      <span class="dot" class:on={server.online && phase === 'open'} aria-hidden="true"></span>
      <span class="hdr">{$tr('desk.header', { name: server.name })}</span>
      {#if server.online}<span class="muted">· {statusText}</span>{/if}
    </p>
    {#if server.online}
      <div class="actions">
        <DesktopTools {session} {phase} bind:autoResize />
        {#if !docked}<button class="btn sm" type="button" onclick={popOut}>{$tr('termwin.openWindow')}</button>{/if}
      </div>
    {/if}
  </div>
  {#if !server.online}
    <p class="note">{$tr('desk.offline')}</p>
  {:else}
    <div class="box"><DesktopSession {serverId} {address} bind:phase bind:autoResize bind:this={session} /></div>
  {/if}
</div>

<style>
  .deskTab {
    display: flex;
    flex-direction: column;
    gap: 9px;
    height: calc(100vh - 198px);
    min-height: 378px;
  }

  .deskTab.docked {
    height: 100%;
    min-height: 0;
  }

  .head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 9px;
    flex-wrap: wrap;
  }

  .meta {
    display: flex;
    align-items: center;
    gap: 7px;
    margin: 0;
  }

  .dot {
    width: 9px;
    height: 9px;
    background: var(--muted);
  }

  .dot.on {
    background: var(--good);
  }

  .actions {
    display: flex;
    gap: 7px;
    flex-wrap: wrap;
  }

  .box {
    flex: 1;
    min-height: 0;
    display: flex;
    border: 2px solid var(--line);
  }

  .note {
    margin: 0;
  }
</style>
