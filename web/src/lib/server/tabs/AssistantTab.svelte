<script lang="ts">
  import { untrack } from 'svelte';
  import type { ServerDetail } from '../../../api';
  import { tr } from '../../../i18n';
  import AssistantSession, { type AssistInfo, type AssistPhase } from '../../assistant/AssistantSession.svelte';
  import AssistantStats from '../../assistant/AssistantStats.svelte';
  import AssistantTools from '../../assistant/AssistantTools.svelte';
  import { gotoServerTab, takeAssistSession } from '../../assistant/nav';
  import { openAssistantWindow } from '../../terminal/windows';

  let { server }: { server: ServerDetail } = $props();

  // Derived primitives: the parent replaces `server` on every live metrics event, which must not restart the session.
  const serverId = $derived(server.id);
  const serverName = $derived(server.name);

  let phase = $state<AssistPhase>('connecting');
  let info = $state<AssistInfo | null>(null);
  let screen = $state<AssistantSession | null>(null);
  // The Manage tab can open this tab with a session of its table.
  let session = $state(untrack(() => takeAssistSession(server.id)));

  const statusText = $derived(
    phase === 'connecting' ? $tr('assist.connectingShort') : phase === 'open' ? $tr('assist.open') : $tr('assist.closed'),
  );

  /** Moves the Assistant into a floating window: this tab's own connection ends first (one window per session). */
  function popOut(): void {
    screen?.disconnect();
    setTimeout(() => openAssistantWindow(serverId, serverName, session), 150);
  }
</script>

<div class="asstTab">
  <div class="head">
    <p class="meta">
      <span class="dot" class:on={server.online && phase === 'open'} aria-hidden="true"></span>
      <span class="hdr">{$tr('assist.header', { name: server.name })}</span>
      {#if server.online}<span class="muted">· {statusText}</span>{/if}
    </p>
    {#if server.online}
      <div class="actions">
        <AssistantTools {serverId} bind:session {phase} {screen} onsettings={() => gotoServerTab(serverName, 'manage')} />
        <button class="btn sm" type="button" onclick={popOut}>{$tr('termwin.openWindow')}</button>
      </div>
    {/if}
  </div>
  {#if !server.online}
    <p class="note">{$tr('assist.offline')}</p>
  {:else}
    <div class="box">
      <div class="screenbox"><AssistantSession {serverId} {session} bind:phase bind:info bind:this={screen} /></div>
      <AssistantStats {serverId} {phase} {info} onsettings={() => gotoServerTab(serverName, 'manage')} />
    </div>
  {/if}
</div>

<style>
  .asstTab {
    display: flex;
    flex-direction: column;
    gap: 9px;
    height: calc(100vh - 198px);
    min-height: 378px;
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
    align-items: center;
  }

  .box {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    border: 2px solid var(--line);
  }

  .screenbox {
    flex: 1;
    min-height: 0;
    display: flex;
  }

  .note {
    margin: 0;
  }
</style>
