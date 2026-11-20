<script lang="ts">
  import { api, type LiveEvent, type ServerDetail } from '../../api';
  import { tr } from '../../i18n';
  import { subscribeLive } from '../../live';
  import FilesTab from '../server/tabs/FilesTab.svelte';
  import WindowFrame from './WindowFrame.svelte';
  import type { TermWindow } from './windows';

  /** The "Files" tab of a server in a floating window: the same listing, tree and actions, opening files in editor windows. */
  let { win, top }: { win: TermWindow; top: boolean } = $props();

  const serverId = $derived(win.serverId);

  let detail = $state<ServerDetail | null>(null);
  let failed = $state(false);

  $effect(() => {
    const id = serverId;
    let alive = true;
    detail = null;
    failed = false;
    void api
      .server(id)
      .then((d) => {
        if (alive) detail = d;
      })
      .catch(() => {
        if (alive) failed = true;
      });
    // the window only needs to know whether the computer is reachable
    const unsubscribe = subscribeLive((e: LiveEvent) => {
      if (!alive || !detail || e.type === 'dashboard' || e.server_id !== id) return;
      const online = e.type === 'status' ? e.online : true;
      if (online !== detail.online) detail = { ...detail, online };
    });
    return () => {
      alive = false;
      unsubscribe();
    };
  });
</script>

<WindowFrame {win} {top} ariaLabel={$tr('pult.filesTitle', { name: win.name })}>
  {#snippet title()}
    <span class="dot" class:on={detail?.online} aria-hidden="true"></span>
    <span class="label">{win.name}</span>
    <span class="muted status">· {$tr('pult.winFiles')}</span>
  {/snippet}
  <div class="fill">
    {#if detail}
      <FilesTab server={detail} embedded />
    {:else if failed}
      <p class="muted">{$tr('common.genericError')}</p>
    {:else}
      <p class="muted">{$tr('common.loading')}</p>
    {/if}
  </div>
</WindowFrame>

<style>
  /* The tab sizes itself to the window; its own panes scroll. */
  .fill {
    height: 100%;
    min-height: 0;
    overflow: hidden;
  }
</style>
