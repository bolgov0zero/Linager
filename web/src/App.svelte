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

  type Theme = 'light' | 'dark';
  type Phase = 'loading' | 'login' | 'app';

  const SRV_PREFIX = '#srv-';

  function initialTheme(): Theme {
    const attr = document.documentElement.dataset.theme;
    if (attr === 'light' || attr === 'dark') return attr;
    return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
  }

  /** '#srv-<encoded name>' is the server page; anything else is the dashboard. */
  function parseServerHash(value: string): string | null {
    if (!value.startsWith(SRV_PREFIX)) return null;
    const raw = value.slice(SRV_PREFIX.length);
    if (!raw) return null;
    try {
      return decodeURIComponent(raw);
    } catch {
      return null;
    }
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

  const serverName = $derived(parseServerHash(hash));
  const clock = $derived(lastLive ? fmtClock(lastLive) : '--:--:--');

  function onHashChange(): void {
    hash = location.hash;
    if (!hash.startsWith(SRV_PREFIX)) openedFromApp = false;
  }

  function openServer(name: string): void {
    openedFromApp = true;
    location.hash = `${SRV_PREFIX}${encodeURIComponent(name)}`;
  }

  function backToDashboard(): void {
    if (openedFromApp) history.back();
    else location.hash = '';
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
    return () => {
      window.removeEventListener('hashchange', onHashChange);
      unsubscribeLive?.();
    };
  });
</script>

<header class="top">
  <div class="brand">
    <h1>{$tr('app.title')}</h1>
    {#if phase === 'app'}
      <p class="live mono small">
        <span class="dot" class:on={$liveConnected} aria-hidden="true"></span>{$tr('dash.live', { time: clock })}
      </p>
    {/if}
  </div>
  <div class="controls">
    {#if phase === 'app' && serverName === null}
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
    {#if serverName !== null}
      {#key serverName}
        <ServerPage name={serverName} onBack={backToDashboard} />
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

  .brand h1 {
    margin: 0;
    font-size: 26px;
    letter-spacing: 0.02em;
    line-height: 1;
  }

  .live {
    display: flex;
    align-items: center;
    margin: 6px 0 0;
    color: var(--muted);
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

  .btn.ink {
    background: var(--ink);
    color: var(--surface);
  }

  .seg {
    display: flex;
    gap: 6px;
  }

  main {
    padding: 24px 16px;
  }
</style>
