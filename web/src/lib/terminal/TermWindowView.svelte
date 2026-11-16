<script lang="ts">
  import { tr } from '../../i18n';
  import KeyIcon from './KeyIcon.svelte';
  import SnippetMenu from './SnippetMenu.svelte';
  import { requestSettings } from '../settings/request';
  import TermIcon from './TermIcon.svelte';
  import TermUserMenu from './TermUserMenu.svelte';
  import TermSession, { type TermPhase } from './TermSession.svelte';
  import WindowFrame from './WindowFrame.svelte';
  import { closeTermWindow, setTermUser, type TermWindow } from './windows';

  let { win, top, canUsers = false }: { win: TermWindow; top: boolean; canUsers?: boolean } = $props();

  let phase = $state<TermPhase>('connecting');
  let keys = $state(false);
  let session = $state<TermSession | null>(null);

  // The key and server never change for a window; deriving them keeps the session from restarting
  // when the store replaces the window object on every move.
  const serverId = $derived(win.serverId);
  const key = $derived(win.key);

  const statusText = $derived(
    phase === 'connecting' ? $tr('term.connecting') : phase === 'open' ? $tr('term.open') : $tr('term.closed'),
  );
</script>

<WindowFrame {win} {top} ariaLabel={$tr('termwin.title', { name: win.name })} onrefocus={() => session?.focus()}>
  {#snippet title()}
    <span class="dot" class:on={phase === 'open'} aria-hidden="true"></span>
    <span class="label">{win.name}</span>
    <span class="muted status">· {statusText}</span>
  {/snippet}
  {#snippet tools()}
    {#if canUsers}
      <TermUserMenu {serverId} current={win.asUser ?? ''} onpick={(u) => setTermUser(key, u)} />
    {/if}
    <SnippetMenu serverId={serverId} oninsert={(text, run) => session?.insert(text, run)} onmanage={() => requestSettings('snippets')} />
    <button class="btn sm icon" class:ink={keys} type="button" title={$tr('term.keys')} aria-label={$tr('term.keys')} aria-pressed={keys} onclick={() => (keys = !keys)}><KeyIcon /></button>
    {#if phase === 'open'}
      <button class="btn sm" type="button" title={$tr('term.clipboardHint')} onclick={() => session?.copy()}><span class="ic"><TermIcon name="copy" /></span><span class="tx">{$tr('term.copy')}</span></button>
      <button class="btn sm" type="button" title={$tr('term.clipboardHint')} onclick={() => session?.paste()}><span class="ic"><TermIcon name="paste" /></span><span class="tx">{$tr('term.paste')}</span></button>
    {/if}
  {/snippet}
  <TermSession {serverId} asUser={win.asUser ?? ''} bind:phase bind:keys bind:this={session} onlimit={() => closeTermWindow(key)} />
</WindowFrame>

<style>
  /* Phones (full-screen windows): copy and paste are icons, the words stay for screen readers. */
  .ic {
    display: none;
  }

  @media (max-width: 720px) {
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
</style>
