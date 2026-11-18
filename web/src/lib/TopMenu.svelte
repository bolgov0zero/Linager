<script lang="ts">
  import { locale, tr, type Locale } from '../i18n';

  /**
   * The menu button of the top bar and its drop-down: theme, language, edit mode, settings, log out.
   * Theme and language switch in place and leave the menu open; the other rows close it.
   */
  let {
    dark,
    signedIn,
    isAdmin,
    canEdit,
    editing,
    updatesCount,
    ontheme,
    onlocale,
    onedit,
    onsettings,
    onlogout,
  }: {
    dark: boolean;
    signedIn: boolean;
    isAdmin: boolean;
    /** The dashboard of an administrator: the only place where the cards can be edited. */
    canEdit: boolean;
    editing: boolean;
    updatesCount: number;
    ontheme: () => void;
    onlocale: (next: Locale) => void;
    onedit: () => void;
    onsettings: () => void;
    onlogout: () => void;
  } = $props();

  let open = $state(false);
  let root = $state<HTMLDivElement | null>(null);
  let button = $state<HTMLButtonElement | null>(null);

  function close(refocus = false): void {
    open = false;
    if (refocus) button?.focus();
  }

  function run(action: () => void): void {
    close();
    action();
  }

  function onOutside(event: PointerEvent): void {
    if (open && root && !root.contains(event.target as Node)) close();
  }

  function onkeydown(event: KeyboardEvent): void {
    if (open && event.key === 'Escape') close(true);
  }
</script>

<svelte:window onpointerdown={onOutside} {onkeydown} />

<div class="topmenu" bind:this={root}>
  <button
    bind:this={button}
    class="btn menu-btn"
    class:ink={open}
    type="button"
    aria-label={$tr('app.menu')}
    aria-haspopup="menu"
    aria-expanded={open}
    onclick={() => (open = !open)}>
    <svg viewBox="0 0 8 8" width="16" height="16" shape-rendering="crispEdges" aria-hidden="true">
      <rect x="0" y="1" width="8" height="1.5" fill="currentColor" />
      <rect x="0" y="3.25" width="8" height="1.5" fill="currentColor" />
      <rect x="0" y="5.5" width="8" height="1.5" fill="currentColor" />
    </svg>
    {#if signedIn && isAdmin && updatesCount > 0}<span class="dotmark" aria-hidden="true"></span>{/if}
  </button>

  {#if open}
    <div class="panel" role="menu" aria-label={$tr('app.menu')}>
      <button class="row" type="button" role="menuitem" onclick={ontheme}>
        <span class="lab">{$tr('more.theme')}</span>
        <span class="val">{dark ? $tr('theme.dark') : $tr('theme.light')}</span>
      </button>
      <button class="row" type="button" role="menuitem" onclick={() => onlocale($locale === 'ru' ? 'en' : 'ru')}>
        <span class="lab">{$tr('more.language')}</span>
        <span class="val">{$locale === 'ru' ? $tr('lang.ru') : $tr('lang.en')}</span>
      </button>
      {#if signedIn}
        {#if canEdit}
          <button class="row" type="button" role="menuitem" onclick={() => run(onedit)}>
            <span class="lab">{editing ? $tr('dash.done') : $tr('dash.edit')}</span>
          </button>
        {/if}
        <button class="row" type="button" role="menuitem" onclick={() => run(onsettings)}>
          <span class="lab">{isAdmin ? $tr('app.settings') : $tr('app.profile')}</span>
          {#if isAdmin && updatesCount > 0}<span class="badge">{updatesCount}</span>{/if}
        </button>
        <button class="row logout" type="button" role="menuitem" onclick={() => run(onlogout)}>
          <span class="lab">{$tr('app.logout')}</span>
        </button>
      {/if}
    </div>
  {/if}
</div>

<style>
  .topmenu {
    position: relative;
  }

  .menu-btn {
    position: relative;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-height: 100%;
  }

  /* A pending update is visible even while the menu is closed. */
  .dotmark {
    position: absolute;
    top: -5px;
    right: -5px;
    width: 9px;
    height: 9px;
    background: var(--warn);
    border: 2px solid var(--surface);
  }

  .panel {
    position: absolute;
    top: calc(100% + 7px);
    right: 0;
    z-index: 30;
    min-width: 198px;
    background: var(--surface);
    border: 2px solid var(--line);
    box-shadow: 4px 4px 0 var(--shade);
  }

  .row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 14px;
    width: 100%;
    min-height: 36px;
    padding: 0 13px;
    background: none;
    border: 0;
    border-top: 2px dashed var(--seg-off);
    color: var(--ink);
    font-family: var(--font-display);
    font-size: 14px;
    text-align: left;
    cursor: pointer;
  }

  .row:first-child {
    border-top: 0;
  }

  .row:hover,
  .row:focus-visible {
    background: var(--ink);
    color: var(--surface);
    outline: none;
  }

  .val {
    color: var(--muted);
  }

  .row:hover .val,
  .row:focus-visible .val {
    color: inherit;
  }

  /* Log out stands apart: a solid line above it and the red of dangerous actions. */
  .row.logout {
    border-top: 2px solid var(--line);
    color: var(--crit);
  }

  .row.logout:hover,
  .row.logout:focus-visible {
    background: var(--crit);
    color: var(--surface);
  }

  .badge {
    background: var(--warn);
    color: #fff;
    line-height: 1.1;
    min-width: 1.4em;
    padding: 0 4px;
    border: 2px solid var(--warn);
    text-align: center;
  }
</style>
