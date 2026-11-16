<script lang="ts">
  import { onMount } from 'svelte';
  import { api, ApiError, isUnauthorized, type PasskeyRow, type Session, type TotpSetup } from '../../api';
  import { locale, t, tr } from '../../i18n';
  import { showToast } from '../toast';
  import { createPasskey, passkeyCancelled, passkeysSupported } from '../webauthn';
  import { formatDateTime } from '../server/format';

  let {
    session,
    onrefresh,
    onauthlost,
  }: { session: Session; onrefresh: () => Promise<void>; onauthlost: () => void } = $props();

  const MIN_PASSWORD = 12;

  // ---- password ----
  let curPw = $state('');
  let newPw = $state('');
  let newPw2 = $state('');
  let pwBusy = $state(false);
  let pwError = $state<string | null>(null);

  // ---- 2FA ----
  let setup = $state<TotpSetup | null>(null);
  let code = $state('');
  let disablePw = $state('');
  let tfBusy = $state(false);
  let tfError = $state<string | null>(null);

  // ---- recovery codes (shown once) ----
  let codes = $state<string[] | null>(null);
  let regenOpen = $state(false);
  let regenPw = $state('');
  let regenBusy = $state(false);
  let regenError = $state<string | null>(null);

  // ---- passkeys ----
  let passkeys = $state<PasskeyRow[]>([]);
  let pkName = $state('');
  let pkBusy = $state(false);
  let pkError = $state<string | null>(null);
  let removing = $state<PasskeyRow | null>(null);
  let removePw = $state('');
  const pkOk = passkeysSupported();

  const recoveryLeft = $derived(session.recovery_left ?? 0);

  function failText(e: unknown, key: string): string {
    if (e instanceof ApiError) {
      if (e.status === 0) return t('common.networkError');
      if (e.status === 429) return e.serverMessage ?? t('common.genericError');
      if (e.status >= 400 && e.status < 500) return e.status === 403 || e.status === 422 ? t(key) : (e.serverMessage ?? t(key));
    }
    return t('common.genericError');
  }

  function handle(e: unknown): boolean {
    if (isUnauthorized(e)) {
      onauthlost();
      return true;
    }
    return false;
  }

  async function loadPasskeys(): Promise<void> {
    try {
      passkeys = (await api.passkeys()).passkeys;
    } catch (e) {
      handle(e);
    }
  }
  onMount(() => void loadPasskeys());

  async function changePassword(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    if (pwBusy) return;
    pwError = null;
    if (newPw.length < MIN_PASSWORD) {
      pwError = t('profile.password.tooShort');
      return;
    }
    if (newPw !== newPw2) {
      pwError = t('profile.password.mismatch');
      return;
    }
    pwBusy = true;
    try {
      await api.changePassword(curPw, newPw);
      curPw = newPw = newPw2 = '';
      showToast(t('profile.password.done'));
    } catch (e) {
      if (!handle(e)) pwError = failText(e, 'profile.password.wrong');
    } finally {
      pwBusy = false;
    }
  }

  // ---- 2FA ----
  async function startSetup(): Promise<void> {
    tfBusy = true;
    tfError = null;
    try {
      setup = await api.totpSetup();
      code = '';
    } catch (e) {
      if (!handle(e)) tfError = t('common.genericError');
    } finally {
      tfBusy = false;
    }
  }

  async function enable(): Promise<void> {
    const value = code.trim();
    if (!value || tfBusy) return;
    tfBusy = true;
    tfError = null;
    try {
      const res = await api.totpEnable(value);
      setup = null;
      code = '';
      codes = res.recovery_codes;
      showToast(t('security.enabled'));
      await onrefresh();
    } catch (e) {
      if (!handle(e)) tfError = failText(e, 'security.invalidCode');
    } finally {
      tfBusy = false;
    }
  }

  async function disable(): Promise<void> {
    if (!disablePw || tfBusy) return;
    tfBusy = true;
    tfError = null;
    try {
      await api.totpDisable(disablePw);
      disablePw = '';
      codes = null;
      showToast(t('security.disabled'));
      await onrefresh();
    } catch (e) {
      if (!handle(e)) tfError = failText(e, 'security.invalidPassword');
    } finally {
      tfBusy = false;
    }
  }

  async function copy(text: string): Promise<void> {
    try {
      await navigator.clipboard.writeText(text);
      showToast(t('common.copied'));
    } catch {
      showToast(t('common.copyFailed'));
    }
  }

  function groupSecret(secret: string): string {
    return secret.replace(/\s+/g, '').match(/.{1,4}/g)?.join(' ') ?? secret;
  }

  // ---- recovery codes ----
  async function regenerate(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    if (!regenPw || regenBusy) return;
    regenBusy = true;
    regenError = null;
    try {
      codes = (await api.recoveryRegenerate(regenPw)).recovery_codes;
      regenPw = '';
      regenOpen = false;
      await onrefresh();
    } catch (e) {
      if (!handle(e)) regenError = failText(e, 'security.invalidPassword');
    } finally {
      regenBusy = false;
    }
  }

  function downloadCodes(): void {
    if (!codes) return;
    const text = `Linager recovery codes for ${session.login}\n\n${codes.join('\n')}\n`;
    const url = URL.createObjectURL(new Blob([text], { type: 'text/plain' }));
    const a = document.createElement('a');
    a.href = url;
    a.download = 'linager-recovery-codes.txt';
    a.click();
    URL.revokeObjectURL(url);
  }

  // ---- passkeys ----
  async function addPasskey(): Promise<void> {
    if (pkBusy) return;
    pkBusy = true;
    pkError = null;
    try {
      const begin = await api.passkeyBegin();
      const response = await createPasskey(begin.options.publicKey);
      await api.passkeyFinish(begin.id, pkName.trim(), response);
      pkName = '';
      showToast(t('passkeys.added'));
      await loadPasskeys();
      await onrefresh();
    } catch (e) {
      if (handle(e) || passkeyCancelled(e)) return;
      pkError = e instanceof ApiError && e.serverMessage ? e.serverMessage : t('passkeys.failed');
    } finally {
      pkBusy = false;
    }
  }

  async function removePasskey(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    const target = removing;
    if (!target || !removePw || pkBusy) return;
    pkBusy = true;
    pkError = null;
    try {
      await api.passkeyDelete(target.id, removePw);
      removing = null;
      removePw = '';
      showToast(t('passkeys.removed'));
      await loadPasskeys();
      await onrefresh();
    } catch (e) {
      if (!handle(e)) pkError = failText(e, 'security.invalidPassword');
    } finally {
      pkBusy = false;
    }
  }
</script>

<div class="profile">
  <h3 class="who">{$tr('profile.you', { login: session.login })}</h3>

  <section class="card stack">
    <h4>{$tr('profile.password.title')}</h4>
    <form class="stack" onsubmit={changePassword}>
      <!-- the login is here so a password manager updates the right entry -->
      <input type="text" name="username" autocomplete="username" value={session.login} hidden readonly />
      <label class="field">
        <span class="label">{$tr('profile.password.current')}</span>
        <input class="input" type="password" autocomplete="current-password" required bind:value={curPw} />
      </label>
      <label class="field">
        <span class="label">{$tr('profile.password.new')}</span>
        <input class="input" type="password" autocomplete="new-password" required bind:value={newPw} />
      </label>
      <label class="field">
        <span class="label">{$tr('profile.password.repeat')}</span>
        <input class="input" type="password" autocomplete="new-password" required bind:value={newPw2} />
      </label>
      {#if pwError}<p class="error" role="alert">{pwError}</p>{/if}
      <div class="row end">
        <button class="btn primary" type="submit" disabled={pwBusy}>{$tr('profile.password.submit')}</button>
      </div>
    </form>
  </section>

  <section class="card stack">
    <h4>{$tr('profile.2fa.title')}</h4>
    {#if session.totp_enabled}
      <p>{$tr('security.totpOn')}</p>
      <form class="stack" onsubmit={(e) => { e.preventDefault(); void disable(); }}>
        <label class="field">
          <span class="label">{$tr('security.password')}</span>
          <input class="input" type="password" autocomplete="current-password" required bind:value={disablePw} />
        </label>
        {#if tfError}<p class="error" role="alert">{tfError}</p>{/if}
        <div class="row end">
          <button class="btn danger" type="submit" disabled={tfBusy || !disablePw}>{$tr('security.disable')}</button>
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
        <button class="btn" type="button" onclick={() => void copy(setup?.uri ?? '')}>{$tr('common.copy')}</button>
      </div>
      <form class="stack" onsubmit={(e) => { e.preventDefault(); void enable(); }}>
        <label class="field">
          <span class="label">{$tr('security.code')}</span>
          <input class="input mono" type="text" inputmode="numeric" autocomplete="one-time-code" maxlength="6" bind:value={code} />
        </label>
        {#if tfError}<p class="error" role="alert">{tfError}</p>{/if}
        <div class="row end">
          <button class="btn primary" type="submit" disabled={tfBusy || code.trim().length !== 6}>{$tr('security.confirmEnable')}</button>
        </div>
      </form>
    {:else}
      <p>{$tr('security.totpOff')}</p>
      {#if tfError}<p class="error" role="alert">{tfError}</p>{/if}
      <div class="row end">
        <button class="btn primary" type="button" disabled={tfBusy} onclick={() => void startSetup()}>{$tr('security.enable')}</button>
      </div>
    {/if}
  </section>

  {#if session.totp_enabled || codes}
    <section class="card stack">
      <h4>{$tr('recovery.title')}</h4>
      {#if codes}
        <p class="warn">{$tr('recovery.shown')}</p>
        <ul class="codes mono">
          {#each codes as c (c)}<li>{c}</li>{/each}
        </ul>
        <div class="row">
          <button class="btn" type="button" onclick={() => void copy(codes?.join('\n') ?? '')}>{$tr('recovery.copy')}</button>
          <button class="btn" type="button" onclick={downloadCodes}>{$tr('recovery.download')}</button>
          <button class="btn primary" type="button" onclick={() => (codes = null)}>{$tr('recovery.saved')}</button>
        </div>
      {:else}
        <p>{$tr('recovery.hint')}</p>
        <p class:warn={recoveryLeft <= 2}>
          {$tr('recovery.left', { n: recoveryLeft })}{#if recoveryLeft <= 2}{' · ' + $tr('recovery.low')}{/if}
        </p>
        {#if regenOpen}
          <form class="stack" onsubmit={regenerate}>
            <p class="muted small">{$tr('recovery.regenerateHint')}</p>
            <label class="field">
              <span class="label">{$tr('security.password')}</span>
              <input class="input" type="password" autocomplete="current-password" required bind:value={regenPw} />
            </label>
            {#if regenError}<p class="error" role="alert">{regenError}</p>{/if}
            <div class="row end">
              <button class="btn" type="button" onclick={() => { regenOpen = false; regenPw = ''; regenError = null; }}>{$tr('common.cancel')}</button>
              <button class="btn primary" type="submit" disabled={regenBusy || !regenPw}>{$tr('recovery.regenerate')}</button>
            </div>
          </form>
        {:else}
          <div class="row end">
            <button class="btn" type="button" onclick={() => (regenOpen = true)}>{$tr('recovery.regenerate')}</button>
          </div>
        {/if}
      {/if}
    </section>
  {/if}

  <section class="card stack">
    <h4>{$tr('passkeys.title')}</h4>
    <p class="muted">{$tr('passkeys.hint')}</p>
    {#if !pkOk}<p class="notice">{$tr('passkeys.unsupported')}</p>{/if}
    {#if passkeys.length === 0}
      <p class="muted">{$tr('passkeys.empty')}</p>
    {:else}
      <ul class="pklist">
        {#each passkeys as pk (pk.id)}
          <li class="pk">
            <div>
              <strong>{pk.name}</strong>
              <p class="muted small">
                {$tr('passkeys.created', { date: formatDateTime(pk.created_at, $locale) })} ·
                {pk.last_used_at
                  ? $tr('passkeys.used', { date: formatDateTime(pk.last_used_at, $locale) })
                  : $tr('passkeys.never')}
              </p>
            </div>
            <button class="btn danger" type="button" onclick={() => { removing = pk; removePw = ''; pkError = null; }}>{$tr('passkeys.remove')}</button>
          </li>
        {/each}
      </ul>
    {/if}
    {#if removing}
      <form class="stack confirm" onsubmit={removePasskey}>
        <p>{$tr('passkeys.removeTitle', { name: removing.name })}</p>
        <label class="field">
          <span class="label">{$tr('passkeys.confirmPassword')}</span>
          <input class="input" type="password" autocomplete="current-password" required bind:value={removePw} />
        </label>
        <div class="row end">
          <button class="btn" type="button" onclick={() => (removing = null)}>{$tr('common.cancel')}</button>
          <button class="btn danger" type="submit" disabled={pkBusy || !removePw}>{$tr('passkeys.remove')}</button>
        </div>
      </form>
    {/if}
    {#if pkError}<p class="error" role="alert">{pkError}</p>{/if}
    <form class="row add" onsubmit={(e) => { e.preventDefault(); void addPasskey(); }}>
      <input class="input grow" type="text" maxlength="60" placeholder={$tr('passkeys.name')} bind:value={pkName} disabled={!pkOk} />
      <button class="btn primary" type="submit" disabled={pkBusy || !pkOk}>{$tr('passkeys.add')}</button>
    </form>
  </section>
</div>

<style>
  .profile {
    display: flex;
    flex-direction: column;
    gap: 16px;
    min-width: 0;
  }

  .who,
  h4 {
    margin: 0;
  }

  h4 {
    font-size: 16px;
    color: var(--muted);
    text-transform: uppercase;
    letter-spacing: 0.06em;
  }

  .secret {
    margin: 0;
    font-size: 18px;
    letter-spacing: 0.12em;
    user-select: all;
  }

  .codes {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(150px, 1fr));
    gap: 6px 16px;
    margin: 0;
    padding: 12px;
    list-style: none;
    border: 2px dashed var(--line);
    font-size: 16px;
    letter-spacing: 0.08em;
    user-select: all;
  }

  .warn {
    margin: 0;
    color: var(--warn);
  }

  .notice {
    margin: 0;
    padding: 8px 12px;
    border: 2px solid var(--warn);
    color: var(--warn);
  }

  .pklist {
    display: flex;
    flex-direction: column;
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .pk {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 8px 0;
    border-bottom: 1px dashed var(--seg-off);
  }

  .pk p {
    margin: 2px 0 0;
  }

  .confirm {
    padding: 12px;
    border: 2px solid var(--crit);
  }

  .add {
    gap: 8px;
  }

  /* the field and the button of this row are the same height */
  .add .input,
  .add .btn {
    height: 36px;
    padding-block: 0;
  }

  .grow {
    flex: 1;
    min-width: 0;
  }

  /* Phones: one column of cards, full-width buttons one under another. */
  @media (max-width: 720px) {
    .profile {
      gap: 12px;
    }

    .who {
      font-size: 20px;
    }

    .profile .row {
      flex-direction: column;
      align-items: stretch;
    }

    .profile .row > .btn {
      width: 100%;
    }

    .add .input,
    .add .btn {
      height: auto;
      min-height: 44px;
    }

    .pk {
      flex-direction: column;
      align-items: stretch;
    }

    .pk .btn {
      width: 100%;
    }

    .codes {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
  }
</style>
