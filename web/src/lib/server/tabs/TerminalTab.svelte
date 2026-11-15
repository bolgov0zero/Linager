<script lang="ts">
  import type { ServerDetail } from '../../../api';
  import { tr } from '../../../i18n';
  import TermSession, { type TermPhase } from '../../terminal/TermSession.svelte';
  import KeyIcon from '../../terminal/KeyIcon.svelte';
  import TermIcon from '../../terminal/TermIcon.svelte';
  import { openTermWindow } from '../../terminal/windows';

  let { server }: { server: ServerDetail } = $props();

  let phase = $state<TermPhase>('connecting');
  let keys = $state(false);
  let session = $state<TermSession | null>(null);

  // Derived primitives: the parent replaces the `server` object on every live metrics event; passing
  // `server.*` straight through would restart the terminal each time (losing the shell).
  const serverId = $derived(server.id);
  const isOnline = $derived(server.online);

  /** Moves the terminal into a floating window: the tab's own session ends first so the per-server limit is not hit. */
  function popOut(): void {
    session?.close();
    setTimeout(() => openTermWindow(serverId, server.name), 150);
  }

  const statusText = $derived(
    phase === 'connecting' ? $tr('term.connecting') : phase === 'open' ? $tr('term.open') : $tr('term.closed'),
  );
</script>

<div class="term">
  <div class="head">
    <p class="meta">
      <span class="dot" class:on={server.online && phase === 'open'} aria-hidden="true"></span>
      <span>{$tr('term.header', { name: server.name })}</span>
      {#if server.online}
        <span class="muted">· {statusText}</span>
      {/if}
    </p>
    {#if server.online && phase !== 'closed'}
      <div class="actions">
        <button class="btn" class:ink={keys} type="button" title={$tr('term.keys')} aria-label={$tr('term.keys')} aria-pressed={keys} onclick={() => (keys = !keys)}><KeyIcon /></button>
        <button class="btn" type="button" title={$tr('term.clipboardHint')} onclick={() => session?.copy()}><span class="ic"><TermIcon name="copy" /></span><span class="tx">{$tr('term.copy')}</span></button>
        <button class="btn" type="button" title={$tr('term.clipboardHint')} onclick={() => session?.paste()}><span class="ic"><TermIcon name="paste" /></span><span class="tx">{$tr('term.paste')}</span></button>
        <button class="btn" type="button" title={$tr('termwin.openWindow')} onclick={popOut}><span class="ic"><TermIcon name="window" /></span><span class="tx">{$tr('termwin.openWindow')}</span></button>
        <button class="btn" type="button" title={$tr('term.close')} onclick={() => session?.close()}><span class="ic"><TermIcon name="close" /></span><span class="tx">{$tr('term.close')}</span></button>
      </div>
    {/if}
  </div>

  {#if !isOnline}
    <p class="note">{$tr('term.offline')}</p>
  {:else}
    <div class="box">
      <TermSession {serverId} bind:phase bind:keys bind:this={session} />
    </div>
  {/if}
</div>

<style>
  /* Phones: the buttons are icons, the words stay for screen readers. */
  .ic {
    display: none;
  }

  @media (max-width: 600px) {
    .actions .btn {
      display: inline-flex;
      align-items: center;
      justify-content: center;
      padding: 6px 12px;
    }

    .ic {
      display: inline-flex;
    }

    .tx {
      position: absolute;
      width: 1px;
      height: 1px;
      overflow: hidden;
      clip: rect(0 0 0 0);
    }
  }

  .actions {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
  }

  .term {
    display: flex;
    flex-direction: column;
    gap: 12px;
    min-width: 0;
  }

  .head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
  }

  .meta {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
    margin: 0;
    font-size: 12px;
  }

  .dot {
    flex: none;
    width: 10px;
    height: 10px;
    border: 2px solid var(--line);
    background: var(--muted);
  }

  .dot.on {
    background: var(--good);
  }

  /* The screen takes the fixed height; the reconnect row (when closed) adds below it. */
  .box :global(.screen) {
    flex: none;
    height: 420px;
  }

  .note {
    margin: 0;
    padding: 24px 0;
    text-align: center;
    color: var(--muted);
  }

  @media (max-width: 600px) {
    .box :global(.screen) {
      height: 320px;
    }
  }
</style>
