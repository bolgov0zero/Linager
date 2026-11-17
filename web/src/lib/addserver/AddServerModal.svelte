<script lang="ts">
  import { onMount } from 'svelte';
  import { api, isUnauthorized, type Group } from '../../api';
  import { tr } from '../../i18n';
  import Modal from '../Modal.svelte';
  import ConfirmTab from './ConfirmTab.svelte';
  import LinkTab from './LinkTab.svelte';
  import SshTab from './SshTab.svelte';
  import './addserver.css';

  type Tab = 'confirm' | 'link' | 'ssh';
  const TABS: Tab[] = ['confirm', 'link', 'ssh'];

  let {
    onclose,
    onadded,
    onopen,
    onauthlost,
  }: {
    onclose: () => void;
    /** A server was approved, put on the dashboard or installed: the dashboard reloads. */
    onadded: () => void;
    /** Open the page of a server (by its name). */
    onopen: (name: string) => void;
    onauthlost: () => void;
  } = $props();

  let tab = $state<Tab>('confirm');
  let pending = $state(0);
  let groups = $state<Group[]>([]);
  let content = $state<HTMLElement | undefined>(undefined);

  function select(next: Tab): void {
    tab = next;
    if (content) content.scrollTop = 0;
  }

  onMount(() => {
    void api
      .groups()
      .then((list) => (groups = list))
      .catch((e) => {
        if (isUnauthorized(e)) onauthlost();
      });
  });
</script>

<Modal title={$tr('asv.title')} labelId="add-server-title" {onclose} fixed>
  <div class="asv">
    <nav class="side" aria-label={$tr('asv.nav')}>
      {#each TABS as id (id)}
        <button class="nav" class:active={tab === id} type="button" aria-current={tab === id ? 'page' : undefined} onclick={() => select(id)}>
          <span>{$tr(`asv.tab.${id}`)}</span>
          {#if id === 'confirm'}<span class="cnt" class:hot={pending > 0}>{pending}</span>{/if}
        </button>
      {/each}
    </nav>
    <div class="content" bind:this={content}>
      <!-- All three stay mounted: a link that was just made and a running install must survive a switch of tabs. -->
      <div class="pane" hidden={tab !== 'confirm'}>
        <ConfirmTab active={tab === 'confirm'} bind:count={pending} {onadded} {onauthlost} />
      </div>
      <div class="pane" hidden={tab !== 'link'}>
        <LinkTab active={tab === 'link'} {groups} {onauthlost} />
      </div>
      <div class="pane" hidden={tab !== 'ssh'}>
        <SshTab
          {groups}
          {onadded}
          {onauthlost}
          onopen={(name) => {
            onclose();
            onopen(name);
          }}
        />
      </div>
    </div>
  </div>
</Modal>

<style>
  .asv {
    flex: 1 1 auto;
    min-height: 0;
    display: grid;
    grid-template-columns: 210px minmax(0, 1fr);
    grid-template-rows: minmax(0, 1fr);
    gap: 20px;
    padding: 16px;
  }

  .side {
    display: flex;
    flex-direction: column;
    gap: 6px;
    align-self: start;
  }

  .nav {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    padding: 8px 12px;
    text-align: left;
    background: var(--surface);
    color: var(--ink);
    border: 2px solid var(--line);
    border-radius: 0;
    font-family: var(--font-display);
    font-size: 14px;
    cursor: pointer;
    box-shadow: 3px 3px 0 var(--shade);
  }

  .nav.active {
    background: var(--ink);
    color: var(--surface);
  }

  .cnt {
    display: inline-block;
    min-width: 20px;
    padding: 0 5px;
    border: 2px solid currentColor;
    font-size: 12px;
    line-height: 1.3;
    text-align: center;
  }

  .cnt.hot {
    background: var(--warn);
    border-color: var(--warn);
    color: var(--surface);
  }

  /* The window never grows: only this column scrolls. */
  .content {
    min-width: 0;
    min-height: 0;
    overflow-y: auto;
    padding: 0 6px 8px 0;
    scrollbar-gutter: stable;
    overscroll-behavior: contain;
  }

  .pane[hidden] {
    display: none;
  }

  /* Phone: the tabs are a row on top, the content below fills the sheet. */
  @media (max-width: 720px) {
    .asv {
      grid-template-columns: minmax(0, 1fr);
      grid-template-rows: auto minmax(0, 1fr);
      gap: 12px;
      padding: 12px 12px calc(12px + var(--safe-b));
    }

    .side {
      flex-direction: row;
      flex-wrap: wrap;
    }

    .nav {
      flex: 1 1 auto;
      min-height: 44px;
      padding: 6px 10px;
      font-size: 13px;
    }

    .content {
      padding: 0;
      scrollbar-gutter: auto;
    }
  }
</style>
