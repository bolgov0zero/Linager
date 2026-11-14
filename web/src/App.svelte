<script lang="ts">
  import { onMount } from 'svelte';
  import { api, type Session } from './api';
  import { locale, setLocale, tr, type Locale } from './i18n';
  import Login from './lib/Login.svelte';
  import SecurityModal from './lib/SecurityModal.svelte';
  import Servers from './lib/Servers.svelte';
  import Toast from './lib/Toast.svelte';

  type Theme = 'light' | 'dark';
  type Phase = 'loading' | 'login' | 'app';

  function initialTheme(): Theme {
    const attr = document.documentElement.dataset.theme;
    if (attr === 'light' || attr === 'dark') return attr;
    return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
  }

  let theme = $state<Theme>(initialTheme());
  let phase = $state<Phase>('loading');
  let session = $state<Session | null>(null);
  let securityOpen = $state(false);

  function toggleTheme(): void {
    theme = theme === 'dark' ? 'light' : 'dark';
    document.documentElement.dataset.theme = theme;
  }

  function pickLocale(next: Locale): void {
    setLocale(next);
  }

  function startSession(next: Session): void {
    session = next;
    phase = 'app';
  }

  function endSession(): void {
    session = null;
    securityOpen = false;
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
    void restoreSession();
  });
</script>

<header class="top">
  <div class="brand">
    <h1>{$tr('app.title')}</h1>
  </div>
  <div class="controls">
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
    <Servers onauthlost={endSession} />
  {/if}
</main>

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
</style>
