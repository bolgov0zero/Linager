<script lang="ts">
  import { api, ApiError, type Session } from '../api';
  import { t, tr } from '../i18n';

  let { onlogin }: { onlogin: (session: Session) => void } = $props();

  let login = $state('');
  let password = $state('');
  let useTotp = $state(false);
  let totp = $state('');
  let busy = $state(false);
  let error = $state<string | null>(null);

  function describeFailure(e: unknown): string {
    if (e instanceof ApiError) {
      if (e.status === 401) return t('login.invalid');
      if (e.status === 429) return e.serverMessage ?? t('common.genericError');
      if (e.status === 0) return t('common.networkError');
    }
    return t('common.genericError');
  }

  async function submit(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    if (busy) return;
    busy = true;
    error = null;
    try {
      const session = await api.login(
        login.trim(),
        password,
        useTotp ? totp.trim() : undefined,
      );
      onlogin(session);
    } catch (e) {
      error = describeFailure(e);
    } finally {
      busy = false;
    }
  }
</script>

<section class="card auth">
  <h2>{$tr('login.title')}</h2>
  <form class="stack" onsubmit={submit}>
    <label class="field">
      <span class="label">{$tr('login.login')}</span>
      <input class="input" name="login" autocomplete="username" required bind:value={login} />
    </label>

    <label class="field">
      <span class="label">{$tr('login.password')}</span>
      <input
        class="input"
        name="password"
        type="password"
        autocomplete="current-password"
        required
        bind:value={password}
      />
    </label>

    <label class="check">
      <input type="checkbox" bind:checked={useTotp} />
      <span>{$tr('login.totpToggle')}</span>
    </label>

    {#if useTotp}
      <label class="field">
        <span class="label">{$tr('login.totp')}</span>
        <input
          class="input mono"
          name="totp"
          inputmode="numeric"
          autocomplete="one-time-code"
          maxlength="6"
          bind:value={totp}
        />
      </label>
    {/if}

    {#if error}
      <p class="error" role="alert">{error}</p>
    {/if}

    <div class="row end">
      <button class="btn primary" type="submit" disabled={busy}>
        {busy ? $tr('login.submitting') : $tr('login.submit')}
      </button>
    </div>
  </form>
</section>

<style>
  .auth {
    max-width: 420px;
  }

  .auth h2 {
    margin: 0 0 16px;
    font-size: 22px;
  }
</style>
