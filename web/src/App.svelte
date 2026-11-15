<script lang="ts">
  import { onMount } from 'svelte';
  import { api, type Session } from './api';
  import { liveConnected, subscribeLive } from './live';
  import { locale, setLocale, t, tr, type Locale } from './i18n';
  import Dashboard from './lib/Dashboard.svelte';
  import EnrollModal from './lib/EnrollModal.svelte';
  import Login from './lib/Login.svelte';
  import SecurityModal from './lib/SecurityModal.svelte';
  import Toast from './lib/Toast.svelte';
  import { showToast } from './lib/toast';
  import AddServerModal from './lib/dashboard/AddServerModal.svelte';
  import { fmtClock } from './lib/dashboard/format';
  import ServerPage from './lib/server/ServerPage.svelte';
  import UpdatesPage from './lib/UpdatesPage.svelte';
  import AuditPage from './lib/AuditPage.svelte';
  import TermWindows from './lib/terminal/TermWindows.svelte';
  import { closeAllTermWindows } from './lib/terminal/windows';

  type Theme = 'light' | 'dark';
  type Phase = 'loading' | 'login' | 'app';

  const SRV_PREFIX = '#srv-';

  function initialTheme(): Theme {
    const attr = document.documentElement.dataset.theme;
    if (attr === 'light' || attr === 'dark') return attr;
    return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
  }

  /**
   * '#srv-<encoded name>[/<tab>]' is the server page (the name is URI-encoded, so it never contains a slash);
   * anything else is the dashboard.
   */
  function parseServerHash(value: string): { name: string; tab: string | null } | null {
    if (!value.startsWith(SRV_PREFIX)) return null;
    const rest = value.slice(SRV_PREFIX.length);
    const slash = rest.indexOf('/');
    const raw = slash < 0 ? rest : rest.slice(0, slash);
    const tab = slash < 0 ? '' : rest.slice(slash + 1);
    if (!raw) return null;
    try {
      return { name: decodeURIComponent(raw), tab: tab || null };
    } catch {
      return null;
    }
  }

  function serverHash(name: string, tab?: string): string {
    const base = `${SRV_PREFIX}${encodeURIComponent(name)}`;
    return tab && tab !== 'overview' ? `${base}/${encodeURIComponent(tab)}` : base;
  }

  let theme = $state<Theme>(initialTheme());
  let phase = $state<Phase>('loading');
  let session = $state<Session | null>(null);
  let securityOpen = $state(false);
  let editing = $state(false);
  let addOpen = $state(false);
  let enrollOpen = $state(false);
  let reloadKey = $state(0);
  let menuOpen = $state(false);
  let lastLive = $state<Date | null>(null);
  let hash = $state(location.hash);
  let openedFromApp = false;
  let unsubscribeLive: (() => void) | undefined;

  const serverRoute = $derived(parseServerHash(hash));
  const serverName = $derived(serverRoute?.name ?? null);
  const page = $derived<'updates' | 'audit' | null>(hash === '#updates' ? 'updates' : hash === '#audit' ? 'audit' : null);
  let updatesCount = $state(0);
  const clock = $derived(lastLive ? fmtClock(lastLive) : '--:--:--');

  function onHashChange(): void {
    hash = location.hash;
    menuOpen = false;
    if (!hash.startsWith(SRV_PREFIX)) openedFromApp = false;
  }

  function openServer(name: string, tab?: string): void {
    openedFromApp = true;
    location.hash = serverHash(name, tab);
  }

  /** Keeps the active tab in the hash without adding a history entry (replaceState fires no hashchange). */
  function setServerTab(name: string, tab: string): void {
    const next = serverHash(name, tab);
    if (location.hash === next) return;
    history.replaceState(history.state, '', `${location.pathname}${location.search}${next}`);
    hash = next;
  }

  function backToDashboard(): void {
    if (openedFromApp) history.back();
    else location.hash = '';
  }

  async function refreshUpdatesCount(): Promise<void> {
    try {
      updatesCount = (await api.updatesSummary()).count;
    } catch {
      // not signed in yet or offline: keep the previous number
    }
  }

  function toggleTheme(): void {
    theme = theme === 'dark' ? 'light' : 'dark';
    document.documentElement.dataset.theme = theme;
  }

  function pickLocale(next: Locale): void {
    setLocale(next);
  }

  function toggleEdit(): void {
    editing = !editing;
    if (editing) showToast(t('dash.editHint'));
  }

  function startSession(next: Session): void {
    session = next;
    phase = 'app';
    if (next.totp_required) securityOpen = true; // force the 2FA setup dialog
    void refreshUpdatesCount();
    if (!unsubscribeLive) {
      unsubscribeLive = subscribeLive(() => {
        lastLive = new Date();
      });
    }
  }

  function endSession(): void {
    session = null;
    securityOpen = false;
    editing = false;
    addOpen = false;
    enrollOpen = false;
    closeAllTermWindows();
    unsubscribeLive?.();
    unsubscribeLive = undefined;
    lastLive = null;
    if (hash) {
      history.replaceState(null, '', `${location.pathname}${location.search}`);
      hash = '';
    }
    phase = 'login';
  }

  async function logout(): Promise<void> {
    try {
      await api.logout();
    } catch {
      // Drop the local session even if the server call failed.
    }
    endSession();
  }

  function setTotpEnabled(enabled: boolean): void {
    if (session) session = { ...session, totp_enabled: enabled };
  }

  async function restoreSession(): Promise<void> {
    try {
      startSession(await api.me());
    } catch {
      endSession();
    }
  }

  onMount(() => {
    window.addEventListener('hashchange', onHashChange);
    void restoreSession();
    const counter = setInterval(() => session && void refreshUpdatesCount(), 60_000);
    return () => {
      clearInterval(counter);
      window.removeEventListener('hashchange', onHashChange);
      unsubscribeLive?.();
    };
  });
</script>

<header class="top">
  <div class="brand">
    <svg class="logo" viewBox="0 0 14 14" shape-rendering="crispEdges" aria-hidden="true">
      <rect width="14" height="14" rx="3" fill="#0f1218" />
      <rect x="2" y="3" width="2" height="2" fill="#e6e9ef" />
      <rect x="4" y="5" width="2" height="2" fill="#e6e9ef" />
      <rect x="6" y="7" width="2" height="2" fill="#e6e9ef" />
      <rect x="4" y="9" width="2" height="2" fill="#e6e9ef" />
      <rect x="8" y="10" width="4" height="2" fill="#7d9cff" />
    </svg>
    <h1>{$tr('app.title')}</h1>
  </div>
  <div class="right">
    {#if phase === 'app'}
      <p class="live mono small">
        <span class="dot" class:on={$liveConnected} aria-hidden="true"></span>{$tr('dash.live', { time: clock })}
      </p>
    {/if}
    <button
      class="btn menu-btn"
      class:ink={menuOpen}
      type="button"
      aria-label={$tr('app.menu')}
      aria-expanded={menuOpen}
      onclick={() => (menuOpen = !menuOpen)}>
      <svg viewBox="0 0 8 8" width="16" height="16" shape-rendering="crispEdges" aria-hidden="true">
        <rect x="0" y="1" width="8" height="1.5" fill="currentColor" />
        <rect x="0" y="3.25" width="8" height="1.5" fill="currentColor" />
        <rect x="0" y="5.5" width="8" height="1.5" fill="currentColor" />
      </svg>
    </button>
    {#if menuOpen}
      <button class="scrim" type="button" tabindex="-1" aria-hidden="true" onclick={() => (menuOpen = false)}></button>
    {/if}
    <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
    <div class="controls" class:open={menuOpen} onclick={() => (menuOpen = false)}>
      {#if phase === 'app'}
        <a class="btn" href="#updates">{$tr('app.updates')}{#if updatesCount > 0} <span class="badge">{updatesCount}</span>{/if}</a>
        <a class="btn" href="#audit">{$tr('app.audit')}</a>
      {/if}
      {#if phase === 'app' && serverName === null && page === null}
        <button class="btn" class:ink={editing} type="button" onclick={toggleEdit}>
          {editing ? $tr('dash.done') : $tr('dash.edit')}
        </button>
        <button class="btn primary" type="button" onclick={() => (addOpen = true)}>{$tr('servers.add')}</button>
      {/if}
      {#if phase === 'app'}
        <button class="btn" type="button" onclick={() => (securityOpen = true)}>{$tr('app.security')}</button>
        <button class="btn" type="button" onclick={() => void logout()}>{$tr('app.logout')}</button>
      {/if}
      <button class="btn" type="button" onclick={toggleTheme}>
        {theme === 'dark' ? $tr('theme.light') : $tr('theme.dark')}
      </button>
      <div class="seg" role="group" aria-label="language">
        <button
          class="chip"
          type="button"
          aria-pressed={$locale === 'ru'}
          onclick={() => pickLocale('ru')}>{$tr('lang.ru')}</button>
        <button
          class="chip"
          type="button"
          aria-pressed={$locale === 'en'}
          onclick={() => pickLocale('en')}>{$tr('lang.en')}</button>
      </div>
    </div>
  </div>
</header>

<main class:center={phase === 'login'}>
  {#if phase === 'loading'}
    <p class="muted">{$tr('common.loading')}</p>
  {:else if phase === 'login'}
    <Login onlogin={startSession} />
  {:else if session}
    {#if page === 'updates'}
      <UpdatesPage onBack={() => (location.hash = '')} onchanged={refreshUpdatesCount} />
    {:else if page === 'audit'}
      <AuditPage onBack={() => (location.hash = '')} />
    {:else if serverName !== null}
      {#key serverName}
        <ServerPage
          name={serverName}
          tab={serverRoute?.tab ?? null}
          ontab={(next) => setServerTab(serverName, next)}
          onBack={backToDashboard}
          onagentchanged={refreshUpdatesCount} />
      {/key}
    {:else}
      <Dashboard {editing} {reloadKey} onopen={openServer} onauthlost={endSession} />
    {/if}
  {/if}
</main>

{#if addOpen && session}
  <AddServerModal
    onclose={() => (addOpen = false)}
    onadded={() => (reloadKey += 1)}
    onpairing={() => {
      addOpen = false;
      enrollOpen = true;
    }}
    onauthlost={endSession}
  />
{/if}

{#if enrollOpen && session}
  <EnrollModal
    onclose={() => (enrollOpen = false)}
    onback={() => {
      enrollOpen = false;
      addOpen = true;
    }}
    onchanged={() => (reloadKey += 1)}
    onauthlost={endSession}
  />
{/if}

{#if securityOpen && session}
  <SecurityModal
    totpEnabled={session.totp_enabled}
    onchange={setTotpEnabled}
    onclose={() => (securityOpen = false)}
    onauthlost={endSession}
  />
{/if}

{#if session}
  <TermWindows />
{/if}

<Toast />

<style>
  .top {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 12px 16px;
    background: var(--surface);
    border-bottom: 2px solid var(--line);
    box-shadow: 3px 3px 0 var(--shade);
  }

  /* The top bar stays pinned while the page scrolls (below floating terminals and dialogs). */
  .top {
    position: sticky;
    top: 0;
    z-index: 10;
  }

  .brand {
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .logo {
    width: 32px;
    height: 32px;
    display: block;
  }

  .brand h1 {
    margin: 0;
    font-variation-settings: "ELSH" 40;
    font-size: 26px;
    letter-spacing: 0.02em;
    line-height: 1;
  }

  .live {
    display: flex;
    align-items: center;
    margin: 0 6px 0 0;
    color: var(--muted);
  }

  /* Header links styled as buttons: no underline. */
  a.btn {
    text-decoration: none;
    display: inline-flex;
    align-items: center;
    gap: 4px;
  }

  .dot {
    display: inline-block;
    width: 8px;
    height: 8px;
    margin-right: 6px;
    background: var(--muted);
  }

  .dot.on {
    background: var(--good);
  }

  .right {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
  }

  /* On wide screens the buttons sit in the bar itself; the menu button and its backdrop exist only on phones. */
  .controls {
    display: contents;
  }

  .menu-btn,
  .scrim {
    display: none;
  }

  .seg {
    display: flex;
    gap: 6px;
  }

  /* Phones: a compact bar (logo, live indicator, menu button) that stays pinned; the buttons open as a panel under it. */
  @media (max-width: 720px) {
    .top {
      flex-wrap: nowrap;
      padding: 8px 12px;
    }

    .logo {
      width: 26px;
      height: 26px;
    }

    .brand {
      gap: 8px;
    }

    .brand h1 {
      font-size: 22px;
    }

    .right {
      flex-wrap: nowrap;
      gap: 8px;
    }

    .live {
      margin: 0;
      white-space: nowrap;
    }

    .menu-btn {
      display: inline-flex;
      align-items: center;
      padding: 6px 10px;
    }

    .scrim {
      display: block;
      position: fixed;
      inset: 0;
      z-index: -1;
      border: 0;
      padding: 0;
      background: transparent;
    }

    .controls {
      display: none;
    }

    .controls.open {
      display: grid;
      grid-template-columns: 1fr 1fr;
      gap: 10px;
      position: absolute;
      top: 100%;
      left: 0;
      right: 0;
      max-height: calc(100dvh - 56px);
      overflow-y: auto;
      padding: 12px;
      background: var(--surface);
      border-bottom: 2px solid var(--line);
      box-shadow: 3px 3px 0 var(--shade);
    }

    .controls.open :global(.btn) {
      justify-content: center;
      text-align: center;
    }

    .seg {
      grid-column: 1 / -1;
    }

    .seg .chip {
      flex: 1;
    }
  }

  main {
    padding: 24px 16px;
  }

  /* The sign-in card sits in the middle of the free space under the top bar. */
  main.center {
    display: flex;
    align-items: center;
    justify-content: center;
    min-height: calc(100dvh - 62px); /* the top bar is 62 px high (50 px on phones) */
  }

  @media (max-width: 720px) {
    main.center {
      min-height: calc(100dvh - 50px);
    }
  }
  .badge {
    background: var(--warn);
    border-color: var(--warn);
    color: #fff;
    display: inline-block;
    min-width: 1.4em;
    padding: 0 4px;
    margin-left: 4px;
    border: 2px solid currentColor;
    text-align: center;
  }
</style>
