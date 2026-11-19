<script lang="ts">
  import type { ServerDetail } from '../../../api';
  import { tr } from '../../../i18n';
  import TermSession, { type TermPhase } from '../../terminal/TermSession.svelte';
  import KeyIcon from '../../terminal/KeyIcon.svelte';
  import ServerShortcuts from '../../assistant/ServerShortcuts.svelte';
  import SnippetMenu from '../../terminal/SnippetMenu.svelte';
  import { requestSettings } from '../../settings/request';
  import TermIcon from '../../terminal/TermIcon.svelte';
  import { openTermWindow } from '../../terminal/windows';
  import { viewBox } from '../../terminal/vv';
  import { lockScroll } from '../../ui/scrolllock';
  import { isPhone } from '../../ui/viewport';

  /** docked: the tab fills the Console (lib/pult): it takes the height of its parent and has no "in a window" button. */
  let { server, docked = false }: { server: ServerDetail; docked?: boolean } = $props();

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

  function openSnippetSettings(): void {
    requestSettings('snippets');
  }

  // Phone: the terminal is a full-screen layer, the page under it must stay still.
  $effect(() => {
    if (!$isPhone) return;
    return lockScroll();
  });

  // On a phone with the keyboard up the terminal covers the visible part of the screen (header and bottom bar included).
  const kbd = $derived($isPhone && $viewBox.kbd);

  const statusText = $derived(
    phase === 'connecting' ? $tr('term.connecting') : phase === 'open' ? $tr('term.open') : $tr('term.closed'),
  );
</script>

<div class="term" class:docked class:kbd style:top={kbd ? `${$viewBox.top}px` : undefined} style:height={kbd ? `${$viewBox.height}px` : undefined}>
  <div class="head">
    <p class="meta">
      <span class="dot" class:on={server.online && phase === 'open'} aria-hidden="true"></span>
      <span class="hdr">{$tr('term.header', { name: server.name })}</span>
      {#if server.online}
        <span class="muted">· {statusText}</span>
      {/if}
    </p>
    {#if server.online && phase !== 'closed'}
      <div class="actions">
        <SnippetMenu serverId={server.id} oninsert={(text, run) => session?.insert(text, run)} onmanage={openSnippetSettings} />
        <button class="btn" class:ink={keys} type="button" title={$tr('term.keys')} aria-label={$tr('term.keys')} aria-pressed={keys} onclick={() => (keys = !keys)}><KeyIcon /></button>
        <ServerShortcuts serverId={server.id} serverName={server.name} />
        <button class="btn" type="button" title={$tr('term.clipboardHint')} onclick={() => session?.copy()}><span class="ic"><TermIcon name="copy" /></span><span class="tx">{$tr('term.copy')}</span></button>
        <button class="btn" type="button" title={$tr('term.clipboardHint')} onclick={() => session?.paste()}><span class="ic"><TermIcon name="paste" /></span><span class="tx">{$tr('term.paste')}</span></button>
        {#if !docked}<button class="btn popout" type="button" title={$tr('termwin.openWindow')} onclick={popOut}><span class="ic"><TermIcon name="window" /></span><span class="tx">{$tr('termwin.openWindow')}</span></button>{/if}
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

  @media (max-width: 720px) {
    .actions .btn {
      display: inline-flex;
      align-items: center;
      justify-content: center;
      width: 44px;
      padding: 0;
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

  /* Phone: the tab already fills the screen; a window over it would only stack a second terminal on the first. */
  @media (max-width: 720px) {
    .actions .btn.popout {
      display: none;
    }
  }

  .actions {
    display: flex;
    gap: 7px;
    flex-wrap: wrap;
  }

  .term {
    display: flex;
    flex-direction: column;
    gap: 11px;
    min-width: 0;
  }

  .term.docked {
    height: 100%;
    min-height: 0;
  }

  .head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 7px;
  }

  .meta {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 7px;
    margin: 0;
    font-size: 11px;
  }

  .dot {
    flex: none;
    width: 9px;
    height: 9px;
    border: 2px solid var(--line);
    background: var(--muted);
  }

  .dot.on {
    background: var(--good);
  }

  /* The screen takes the fixed height; the reconnect row (when closed) adds below it. */
  .box :global(.screen) {
    flex: none;
    height: 378px;
  }

  /* In the Console the terminal takes all the room under the header. */
  .term.docked .box {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }

  .term.docked .box :global(.session) {
    flex: 1;
    min-height: 0;
    height: auto;
  }

  .term.docked .box :global(.screen) {
    flex: 1;
    height: auto;
    min-height: 0;
  }

  .note {
    margin: 0;
    padding: 22px 0;
    text-align: center;
    color: var(--muted);
  }

  /* Phones: no frame and no height of its own. The terminal fills everything between the page header and the bottom
     bar (the whole visible area while the keyboard is up); the toolbar is one slim row above it. */
  @media (max-width: 720px) {
    .term {
      position: fixed;
      inset: var(--top-h) 0 calc(var(--bar-h) + var(--safe-b)) 0;
      z-index: 12;
      gap: 0;
      background: var(--bg);
    }

    .term.kbd {
      bottom: auto;
      z-index: 16;
      padding-top: 0;
    }

    /* The area under the key row (the keyboard and its translucent accessory bar) must not show the page. */
    .term.kbd::before {
      content: '';
      position: fixed;
      inset: 0;
      z-index: -1;
      background: var(--surface);
    }

    .head {
      flex: none;
      flex-wrap: nowrap;
      gap: 4px;
      padding: 2px 5px 2px 11px;
      border-bottom: 2px solid var(--line);
      background: var(--surface);
    }

    .meta {
      flex: 1;
      flex-wrap: nowrap;
      min-width: 0;
      gap: 5px;
      overflow: hidden;
      white-space: nowrap;
    }

    .hdr {
      display: none;
    }

    .meta span:not(.dot) {
      overflow: hidden;
      text-overflow: ellipsis;
    }

    .actions {
      flex: none;
      flex-wrap: nowrap;
      gap: 4px;
    }

    .actions .btn,
    .actions :global(.btn) {
      box-shadow: none;
      border-color: transparent;
      background: none;
    }

    .actions .btn.ink {
      border-color: var(--line);
      background: var(--ink);
    }

    .box {
      display: flex;
      flex: 1;
      flex-direction: column;
      min-height: 0;
    }

    .box :global(.session) {
      flex: 1;
      height: auto;
      min-height: 0;
    }

    .box :global(.screen) {
      flex: 1;
      height: auto;
    }

    .note {
      padding: 22px 11px;
    }
  }
</style>
