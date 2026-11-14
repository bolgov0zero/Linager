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
    <svg class="logo" viewBox="0 0 8 8" shape-rendering="crispEdges" aria-hidden="true">
      <rect x="0" y="0" width="8" height="2" fill="var(--ink)" />
      <rect x="0" y="3" width="8" height="2" fill="var(--ink)" />
      <rect x="0" y="6" width="8" height="2" fill="var(--ink)" />
      <rect x="5" y="0" width="1" height="2" fill="var(--surface)" />
      <rect x="6" y="3" width="1" height="2" fill="var(--accent)" />
      <rect x="5" y="6" width="1" height="2" fill="var(--surface)" />
    </svg>
    <h1>{$tr('app.title')}</h1>
  </div>
  <div class="controls">
    {#if phase === 'app'}
      <p class="live mono small">
        <span class="dot" class:on={$liveConnected} aria-hidden="true"></span>{$tr('dash.live', { time: clock })}
      </p>
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
</header>

<main>
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

  /* The top bar stays pinned while the page scrolls (below floating terminals and dialogs). On phones the bar
     wraps into several lines, so there it scrolls away with the page. */
  @media (min-width: 721px) {
    .top {
      position: sticky;
      top: 0;
      z-index: 10;
    }
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

  .controls {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
  }

  .seg {
    display: flex;
    gap: 6px;
  }

  main {
    padding: 24px 16px;
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
