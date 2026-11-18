<script lang="ts">
  import { tick } from 'svelte';
  import { api, type Snippet } from '../../api';
  import { tr } from '../../i18n';
  import { loadSnippets, snippets } from '../snippets';
  import TermIcon from './TermIcon.svelte';

  /**
   * "Snippets" button with a list under it. Clicking a snippet pastes it and presses Enter; the small
   * button at the right of the row only pastes it. The list is moved to <body> so a window or a scroll
   * container does not clip it.
   */
  let {
    serverId,
    oninsert,
    onmanage,
    disabled = false,
  }: {
    serverId: string;
    oninsert: (text: string, run: boolean) => void;
    onmanage?: () => void;
    disabled?: boolean;
  } = $props();

  const uid = `sm-${Math.random().toString(36).slice(2, 9)}`;

  let button = $state<HTMLButtonElement | null>(null);
  let open = $state(false);
  let pos = $state({ left: 0, top: 0, up: false, max: 320 });

  function portal(node: HTMLElement) {
    document.body.appendChild(node);
    return { destroy: () => node.remove() };
  }

  async function show(): Promise<void> {
    if (disabled || !button) return;
    void loadSnippets();
    const r = button.getBoundingClientRect();
    const below = window.innerHeight - r.bottom - 8;
    const above = r.top - 8;
    const up = below < 200 && above > below;
    pos = { left: r.right, top: up ? r.top : r.bottom, up, max: Math.max(140, Math.min(360, up ? above : below)) };
    open = true;
    await tick();
  }

  function hide(): void {
    open = false;
  }

  function run(sn: Snippet): void {
    hide();
    oninsert(sn.command, true);
    void api.snippetRan(sn.id, serverId).catch(() => undefined);
  }

  function paste(sn: Snippet): void {
    hide();
    oninsert(sn.command, false);
  }

  function onOutside(event: PointerEvent): void {
    const target = event.target as Node;
    if (open && !button?.contains(target) && !document.getElementById(`${uid}-list`)?.contains(target)) hide();
  }
</script>

<svelte:window
  onpointerdown={onOutside}
  onresize={hide}
  onkeydown={(e) => e.key === 'Escape' && open && hide()}
  onscrollcapture={(e) => {
    if (open && !(e.target as Node).contains?.(document.getElementById(`${uid}-list`))) hide();
  }}
/>

<button
  bind:this={button}
  class="btn sm sm-btn"
  type="button"
  aria-haspopup="menu"
  aria-expanded={open}
  {disabled}
  onclick={() => (open ? hide() : void show())}
>
  <span class="ic"><TermIcon name="bolt" /></span>
  <span class="tx">{$tr('term.snippets')}</span>
  <span class="arrow" aria-hidden="true"></span>
</button>

{#if open}
  <div use:portal class="sheet-scrim"></div>
  <ul
    use:portal
    id="{uid}-list"
    class="list sel-list"
    role="menu"
    aria-label={$tr('term.snippets')}
    style:left="{pos.left}px"
    style:top="{pos.top}px"
    style:max-height="{pos.max}px"
    style:transform="translateX(-100%){pos.up ? ' translateY(-100%)' : ''}"
  >
    {#each $snippets as sn (sn.id)}
      <li class="row" role="none">
        <button class="run" type="button" role="menuitem" title={sn.command} onclick={() => run(sn)}>{sn.name}</button>
        <button class="paste" type="button" aria-label="{$tr('term.snippetPaste')}: {sn.name}" title={$tr('term.snippetPaste')} onclick={() => paste(sn)}>
          <svg viewBox="0 0 16 16" width="14" height="14" shape-rendering="crispEdges" fill="currentColor" aria-hidden="true">
            <rect x="5" y="1" width="6" height="2" />
            <rect x="3" y="3" width="2" height="11" />
            <rect x="11" y="3" width="2" height="11" />
            <rect x="3" y="12" width="10" height="2" />
            <rect x="6" y="6" width="4" height="2" />
            <rect x="6" y="9" width="4" height="2" />
          </svg>
        </button>
      </li>
    {:else}
      <li class="empty" role="none">{$tr('term.snippetsEmpty')}</li>
    {/each}
    {#if onmanage}
      <li class="manage" role="none">
        <button type="button" role="menuitem" onclick={() => { hide(); onmanage?.(); }}>{$tr('term.snippetsManage')}</button>
      </li>
    {/if}
  </ul>
{/if}

<style>
  .sm-btn {
    display: inline-flex;
    align-items: center;
    gap: 7px;
  }

  .arrow {
    width: 0;
    height: 0;
    border-left: 4px solid transparent;
    border-right: 4px solid transparent;
    border-top: 5px solid currentColor;
  }

  .list {
    position: fixed;
    z-index: 100001;
    min-width: 198px;
    max-width: min(378px, calc(100vw - 14px));
    margin: 0;
    padding: 4px;
    overflow-y: auto;
    list-style: none;
    background: var(--surface);
    color: var(--ink);
    border: 2px solid var(--line);
    box-shadow: 3px 3px 0 var(--shade);
  }

  .row {
    display: flex;
    align-items: stretch;
    gap: 4px;
  }

  .run,
  .paste,
  .manage button {
    border: 0;
    background: none;
    color: inherit;
    font: inherit;
    font-size: 12px;
    cursor: pointer;
  }

  .run {
    flex: 1;
    min-width: 0;
    padding: 7px 9px;
    text-align: left;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .paste {
    flex: none;
    display: inline-flex;
    align-items: center;
    padding: 0 9px;
    border-left: 2px solid var(--seg-off);
    color: var(--muted);
  }

  .run:hover,
  .run:focus-visible,
  .paste:hover,
  .paste:focus-visible,
  .manage button:hover {
    background: var(--ink);
    color: var(--surface);
  }

  .empty {
    padding: 7px 9px;
    color: var(--muted);
    font-size: 12px;
  }

  .manage {
    margin-top: 4px;
    border-top: 2px solid var(--seg-off);
  }

  .manage button {
    width: 100%;
    padding: 7px 9px;
    text-align: left;
    color: var(--muted);
  }

  /* Phones: the button is an icon (the words stay for screen readers) and the list opens as a sheet from the bottom. */
  .ic {
    display: none;
  }

  @media (max-width: 720px) {
    .list {
      min-width: 0;
      max-width: none;
      box-shadow: 0 -4px 0 var(--shade);
    }

    .sm-btn {
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

    .arrow {
      display: none;
    }

    .run,
    .paste,
    .manage button {
      min-height: 50px;
      font-size: 14px;
    }

    .paste {
      min-width: 56px;
      justify-content: center;
    }

    .row {
      gap: 0;
      width: 100%;
    }

    .manage {
      margin-top: 0;
      border-top: 2px dashed var(--seg-off);
    }

    .empty {
      padding: 13px 0;
    }
  }
</style>
