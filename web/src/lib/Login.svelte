<script lang="ts">
  import { api, ApiError, isTotpRequired, type Session } from '../api';
  import { t, tr } from '../i18n';
  import { isPhone } from './ui/viewport';
  import { getPasskey, passkeyCancelled, passkeysSupported } from './webauthn';

  let { onlogin }: { onlogin: (session: Session) => void } = $props();

  // One form for both steps, so password managers see the same login form the whole time: the
  // login and password fields only get hidden on the second step, they are not removed.
  type Step = 'password' | 'totp';

  let step = $state<Step>('password');
  let login = $state('');
  let password = $state('');
  let totp = $state('');
  let useRecovery = $state(false);
  let busy = $state(false);
  let error = $state<string | null>(null);
  const passkeyOk = passkeysSupported();
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
      if (step === 'password' && isTotpRequired(e)) {
        step = 'totp';
        totp = '';
        useRecovery = false;
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
    useRecovery = false;
  }

  function toggleRecovery(): void {
    useRecovery = !useRecovery;
    totp = '';
    error = null;
    totpEl?.focus();
  }

  // Sign in as soon as the sixth digit is typed or filled in by a password manager.
  function onTotpInput(): void {
    if (useRecovery) return;
    totp = totp.replace(/\D/g, '');
    if (totp.length === 6 && !busy) formEl?.requestSubmit();
  }

  /** Sign in with a passkey: no login and no password, the browser shows the passkeys of this site. */
  async function passkeyLogin(): Promise<void> {
    if (busy) return;
    busy = true;
    error = null;
    try {
      const begin = await api.passkeyLoginBegin();
      const response = await getPasskey(begin.options.publicKey);
      onlogin(await api.passkeyLoginFinish(begin.id, response));
    } catch (e) {
      if (!passkeyCancelled(e)) {
        error = e instanceof ApiError && e.status === 429 ? (e.serverMessage ?? t('common.genericError')) : t('login.passkeyFailed');
      }
    } finally {
      busy = false;
    }
  }
</script>

<section class="card auth">
  <h2>{$tr('login.title')}</h2>
  {#if $isPhone}
    <p class="muted sub">{$tr('login.mob.subtitle')}</p>
    {#if step === 'password'}
      <div class="stack pk">
        <button
          class="btn blk"
          type="button"
          disabled={busy || !passkeyOk}
          title={passkeyOk ? undefined : $tr('login.passkeyUnavailable')}
          onclick={() => void passkeyLogin()}
        >
          {$tr('login.passkey')}
        </button>
        <p class="muted small or">{$tr('login.mob.or')}</p>
      </div>
    {/if}
  {/if}
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
        <span class="label">{useRecovery ? $tr('login.recovery') : $tr('login.totp')}</span>
        <input
          class="input mono code"
          id="otp"
          name="otp"
          type="text"
          inputmode={useRecovery ? 'text' : 'numeric'}
          pattern={useRecovery ? undefined : '[0-9]*'}
          autocomplete="one-time-code"
          autocapitalize="characters"
          spellcheck="false"
          maxlength={useRecovery ? 16 : 6}
          required
          bind:this={totpEl}
          bind:value={totp}
          oninput={onTotpInput}
        />
      </label>
      <button class="linkbtn" type="button" onclick={toggleRecovery}>
        {useRecovery ? $tr('login.useTotp') : $tr('login.useRecovery')}
      </button>
    {/if}

    {#if error}
      <p class="error" role="alert">{error}</p>
    {/if}

    <div class="row end buttons" class:phone={$isPhone}>
      {#if step === 'totp'}
        <button class="btn" type="button" disabled={busy} onclick={back}>{$tr('login.back')}</button>
      {:else if !$isPhone}
        <button
          class="btn"
          type="button"
          disabled={busy || !passkeyOk}
          title={passkeyOk ? undefined : $tr('login.passkeyUnavailable')}
          onclick={() => void passkeyLogin()}
        >
          {$tr('login.passkey')}
        </button>
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
    max-width: 378px;
  }

  .auth h2 {
    margin: 0 0 14px;
    font-size: 20px;
  }

  .stack[hidden] {
    display: none;
  }

  .who {
    margin: 0;
  }

  .linkbtn {
    align-self: flex-start;
    padding: 0;
    border: 0;
    background: none;
    color: var(--accent);
    font: inherit;
    font-size: 12px;
    text-decoration: underline;
    cursor: pointer;
  }

  .buttons {
    flex-wrap: wrap;
  }

  .code {
    letter-spacing: 0.3em;
    text-align: center;
    font-size: 18px;
  }

  .sub,
  .or {
    margin: 0;
  }

  /* Phones: no frame, the form is at the top of the free area (the keyboard does not cover it), full-width buttons. */
  @media (max-width: 720px) {
    .auth {
      align-self: flex-start;
      padding: 11px 0 0;
      background: none;
      border: 0;
      box-shadow: none;
    }

    .auth h2 {
      margin: 0 0 4px;
      font-size: 25px;
    }

    .sub {
      margin-bottom: 14px;
    }

    .pk {
      margin-bottom: 14px;
      gap: 9px;
    }

    .or {
      text-align: center;
    }

    .blk {
      width: 100%;
    }

    .buttons {
      flex-direction: column-reverse;
      align-items: stretch;
    }

    .buttons .btn {
      width: 100%;
    }

    .linkbtn {
      min-height: 44px;
      font-size: 13px;
    }

    .code {
      font-size: 22px;
    }
  }
</style>
