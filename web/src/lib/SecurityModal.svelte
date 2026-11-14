<script lang="ts">
  import { api, ApiError, isUnauthorized, type TotpSetup } from '../api';
  import { t, tr } from '../i18n';
  import Modal from './Modal.svelte';
  import { showToast } from './toast';

  let {
    totpEnabled,
    onchange,
    onclose,
    onauthlost,
  }: {
    totpEnabled: boolean;
    onchange: (enabled: boolean) => void;
    onclose: () => void;
    onauthlost: () => void;
  } = $props();

  let setup = $state<TotpSetup | null>(null);
  let code = $state('');
  let password = $state('');
  let busy = $state(false);
  let error = $state<string | null>(null);

  /** Spaced groups of 4 characters make the secret easier to type by hand. */
  function groupSecret(secret: string): string {
    return secret.replace(/\s+/g, '').match(/.{1,4}/g)?.join(' ') ?? secret;
  }

  /** 4xx means the client sent bad data, 0 means no connection, anything else is generic. */
  function fail(e: unknown, clientKey: string): void {
    if (isUnauthorized(e)) {
      onauthlost();
      return;
    }
    if (e instanceof ApiError && e.status === 0) {
      error = t('common.networkError');
    } else if (e instanceof ApiError && e.status >= 400 && e.status < 500) {
      error = t(clientKey);
    } else {
      error = t('common.genericError');
    }
  }

  async function startSetup(): Promise<void> {
    busy = true;
    error = null;
    try {
      setup = await api.totpSetup();
      code = '';
    } catch (e) {
      fail(e, 'common.genericError');
    } finally {
      busy = false;
    }
  }

  async function enable(): Promise<void> {
    const value = code.trim();
    if (!value || busy) return;
    busy = true;
    error = null;
    try {
      await api.totpEnable(value);
      setup = null;
      code = '';
      showToast(t('security.enabled'));
      onchange(true);
    } catch (e) {
      fail(e, 'security.invalidCode');
    } finally {
      busy = false;
    }
  }

  async function disable(): Promise<void> {
    if (!password || busy) return;
    busy = true;
    error = null;
    try {
      await api.totpDisable(password);
      password = '';
      showToast(t('security.disabled'));
      onchange(false);
    } catch (e) {
      fail(e, 'security.invalidPassword');
    } finally {
      busy = false;
    }
  }

  async function copyUri(): Promise<void> {
    if (!setup) return;
    try {
      await navigator.clipboard.writeText(setup.uri);
      showToast(t('common.copied'));
    } catch {
      showToast(t('common.copyFailed'));
    }
  }
</script>

<Modal title={$tr('security.title')} labelId="security-title" {onclose}>
  {#if totpEnabled}
    <p>{$tr('security.totpOn')}</p>
    <form
      class="stack"
      onsubmit={(event) => {
        event.preventDefault();
        void disable();
      }}
    >
      <label class="field">
        <span class="label">{$tr('security.password')}</span>
        <input class="input" type="password" autocomplete="current-password" required bind:value={password} />
      </label>
      {#if error}
        <p class="error" role="alert">{error}</p>
      {/if}
      <div class="row end">
        <button class="btn danger" type="submit" disabled={busy || !password}>{$tr('security.disable')}</button>
      </div>
    </form>
  {:else if setup}
    <p>{$tr('security.setupHint')}</p>

    <div class="field">
      <span class="label">{$tr('security.secret')}</span>
      <p class="secret mono">{groupSecret(setup.secret)}</p>
    </div>

    <div class="field">
      <span class="label">{$tr('security.uri')}</span>
      <input class="input mono" type="text" readonly value={setup.uri} />
    </div>
    <div class="row">
      <button class="btn" type="button" onclick={() => void copyUri()}>{$tr('common.copy')}</button>
    </div>

    <form
      class="stack"
      onsubmit={(event) => {
        event.preventDefault();
        void enable();
      }}
    >
      <label class="field">
        <span class="label">{$tr('security.code')}</span>
        <input
          class="input mono"
          type="text"
          inputmode="numeric"
          autocomplete="one-time-code"
          maxlength="6"
          bind:value={code}
        />
      </label>
      {#if error}
        <p class="error" role="alert">{error}</p>
      {/if}
      <div class="row end">
        <button class="btn primary" type="submit" disabled={busy || code.trim().length !== 6}>
          {$tr('security.confirmEnable')}
        </button>
      </div>
    </form>
  {:else}
    <p>{$tr('security.totpOff')}</p>
    {#if error}
      <p class="error" role="alert">{error}</p>
    {/if}
    <div class="row end">
      <button class="btn primary" type="button" disabled={busy} onclick={() => void startSetup()}>
        {$tr('security.enable')}
      </button>
    </div>
  {/if}
</Modal>

<style>
  .secret {
    margin: 0;
    font-size: 18px;
    letter-spacing: 0.12em;
    user-select: all;
  }
</style>
