<script lang="ts">
  import { tr } from '../../i18n';
  import TermSession, { type TermPhase } from './TermSession.svelte';
  import WindowFrame from './WindowFrame.svelte';
  import { closeTermWindow, type TermWindow } from './windows';

  let { win, top }: { win: TermWindow; top: boolean } = $props();

  let phase = $state<TermPhase>('connecting');
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
    {#if phase === 'open'}
      <button class="btn sm" type="button" title={$tr('term.clipboardHint')} onclick={() => session?.copy()}>{$tr('term.copy')}</button>
      <button class="btn sm" type="button" title={$tr('term.clipboardHint')} onclick={() => session?.paste()}>{$tr('term.paste')}</button>
    {/if}
  {/snippet}
  <TermSession {serverId} bind:phase bind:this={session} onlimit={() => closeTermWindow(key)} />
</WindowFrame>
