<script lang="ts">
  import { api, ApiError, type Session } from '../api';
  import { t, tr } from '../i18n';

  let { onlogin }: { onlogin: (session: Session) => void } = $props();

  /** The server answers 401 with this code when the password is right and the 2FA code is missing. */
  const TOTP_REQUIRED = 'totp_required';

  // One form for both steps, so password managers see the same login form the whole time: the
  // login and password fields only get hidden on the second step, they are not removed.
  type Step = 'password' | 'totp';

  let step = $state<Step>('password');
  let login = $state('');
  let password = $state('');
  let totp = $state('');
  let busy = $state(false);
  let error = $state<string | null>(null);
  let formEl = $state<HTMLFormElement | undefined>(undefined);
  let totpEl = $state<HTMLInputElement | undefined>(undefined);

  function describeFailure(e: unknown): string {
    if (e instanceof ApiError) {
      if (e.status === 401) return t('login.invalid');
      if (e.status === 429) return e.serverMessage ?? t('common.genericError');
      if (e.status === 0) return t('common.networkError');
    }
    return t('common.genericError');
  }

  // The code field gets the focus when the second step opens.
  $effect(() => {
    if (step === 'totp') totpEl?.focus();
  });

  async function submit(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    if (busy) return;
    busy = true;
    error = null;
    try {
      const session = await api.login(login.trim(), password, step === 'totp' ? totp.trim() : undefined);
      onlogin(session);
    } catch (e) {
      if (step === 'password' && e instanceof ApiError && e.status === 401 && e.serverMessage === TOTP_REQUIRED) {
        step = 'totp';
        totp = '';
      } else {
        error = describeFailure(e);
        if (step === 'totp') totp = '';
      }
    } finally {
      busy = false;
    }
  }

  function back(): void {
    step = 'password';
    totp = '';
    error = null;
  }

  // Sign in as soon as the sixth digit is typed or filled in by a password manager.
  function onTotpInput(): void {
    totp = totp.replace(/\D/g, '');
    if (totp.length === 6 && !busy) formEl?.requestSubmit();
  }
</script>

<section class="card auth">
  <h2>{$tr('login.title')}</h2>
  <form class="stack" method="post" action="#" bind:this={formEl} onsubmit={submit}>
    <div class="stack" hidden={step === 'totp'}>
      <label class="field">
        <span class="label">{$tr('login.login')}</span>
        <input
          class="input"
          id="username"
          name="username"
          type="text"
          autocomplete="username"
          autocapitalize="none"
          autocorrect="off"
          spellcheck="false"
          required={step === 'password'}
          bind:value={login}
        />
      </label>

      <label class="field">
        <span class="label">{$tr('login.password')}</span>
        <input
          class="input"
          id="current-password"
          name="password"
          type="password"
          autocomplete="current-password"
          required={step === 'password'}
          bind:value={password}
        />
      </label>
    </div>

    {#if step === 'totp'}
      <p class="muted who">{$tr('login.totpFor', { login: login.trim() })}</p>
      <label class="field">
        <span class="label">{$tr('login.totp')}</span>
        <input
          class="input mono code"
          id="otp"
          name="otp"
          type="text"
          inputmode="numeric"
          pattern="[0-9]*"
          autocomplete="one-time-code"
          maxlength="6"
          required
          bind:this={totpEl}
          bind:value={totp}
          oninput={onTotpInput}
        />
      </label>
    {/if}

    {#if error}
      <p class="error" role="alert">{error}</p>
    {/if}

    <div class="row end">
      {#if step === 'totp'}
        <button class="btn" type="button" disabled={busy} onclick={back}>{$tr('login.back')}</button>
      {/if}
      <button class="btn primary" type="submit" disabled={busy}>
        {busy ? $tr('login.submitting') : step === 'totp' ? $tr('login.totpSubmit') : $tr('login.submit')}
      </button>
    </div>
  </form>
</section>

<style>
  .auth {
    width: 100%;
    max-width: 420px;
  }

  .auth h2 {
    margin: 0 0 16px;
    font-size: 22px;
  }

  .stack[hidden] {
    display: none;
  }

  .who {
    margin: 0;
  }

  .code {
    letter-spacing: 0.3em;
    text-align: center;
    font-size: 20px;
  }
</style>
