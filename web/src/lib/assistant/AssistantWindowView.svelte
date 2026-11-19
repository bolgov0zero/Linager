<script lang="ts">
  import { untrack } from 'svelte';
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

  // How the picture and the window go together. The window follows the size of the picture by default; the person
  // choosing another mode, resizing the window by hand or maximizing it leaves that mode.
  let display = $state<'fit' | 'window' | 'max' | 'real'>('window');
  let boxW = $state(0);
  let boxH = $state(0);
  /** What the window needs around the picture (header, borders, the stats line), measured once. */
  let chrome = $state<{ w: number; h: number } | null>(null);
  let applied = { w: 0, h: 0 };
  const narrow = (): boolean => window.matchMedia('(max-width: 720px)').matches;

  $effect(() => {
    if (!chrome && !win.maximized && boxW > 50 && boxH > 50) chrome = { w: win.w - boxW, h: win.h - boxH };
  });

  /** The window gets the size of the picture (at 100%), as far as the page holds it, and stays inside the page. */
  function fitWindow(pw: number, ph: number): void {
    const c = chrome;
    if (!c) return;
    const vw = window.innerWidth;
    const vh = window.innerHeight;
    const w = Math.max(360, Math.min(pw + c.w, vw - 16));
    const h = Math.max(220, Math.min(ph + c.h, vh - 64));
    applied = { w, h };
    updateTermWindow(win.key, {
      w,
      h,
      x: Math.max(8, Math.min(win.x, vw - w - 8)),
      y: Math.max(8, Math.min(win.y, vh - h - 56)),
      maximized: false,
    });
  }

  $effect(() => {
    if (display !== 'window' || !info || !chrome || narrow()) return;
    const [pw, ph] = [info.w, info.h];
    untrack(() => fitWindow(pw, ph));
  });

  // the person resized the window by hand: the picture is fitted into it from now on
  $effect(() => {
    const [w, h, m] = [win.w, win.h, win.maximized];
    untrack(() => {
      if (display === 'window' && !m && applied.w && (Math.abs(w - applied.w) > 3 || Math.abs(h - applied.h) > 3)) display = 'fit';
    });
  });

  // the maximize button of the header and the mode "fills the browser" are the same thing
  $effect(() => {
    const m = win.maximized;
    untrack(() => {
      if (m && display !== 'max') display = 'max';
      else if (!m && display === 'max') display = 'fit';
    });
  });
  $effect(() => {
    const d = display;
    untrack(() => {
      if (d === 'max' && !win.maximized) updateTermWindow(win.key, { maximized: true });
      else if (d !== 'max' && win.maximized) updateTermWindow(win.key, { maximized: false });
    });
  });

  // the picture is shown at 100% while the window follows it, fitted into the window otherwise
  $effect(() => {
    screen?.setScale(display === 'window' ? 'real' : 'fit');
  });

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
    <AssistantTools {serverId} bind:session={() => session, (v) => updateTermWindow(win.key, { session: v })} {phase} {info} {screen} layout="window" bind:display onsettings={() => gotoServerTab(serverName, 'manage')} />
  {/snippet}
  <div class="col">
    <div class="screenbox" bind:clientWidth={boxW} bind:clientHeight={boxH}><AssistantSession {serverId} {session} bind:phase bind:info bind:this={screen} /></div>
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
