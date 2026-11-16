<script lang="ts">
  import { api, ApiError, type HostUser } from '../../../api';
  import { t, tr } from '../../../i18n';
  import { showToast } from '../../toast';
  import Modal from '../../Modal.svelte';
  import { errorText } from './svc';
  import { genPassword } from './users-util';

  let { serverId, user, onclose }: { serverId: string; user: HostUser; onclose: () => void } = $props();

  let mode = $state<'generate' | 'manual'>('generate');
  let generated = $state(genPassword());
  let manual = $state('');
  let busy = $state(false);
  let error = $state<string | null>(null);

  const password = $derived(mode === 'generate' ? generated : manual);
  const valid = $derived(password.length >= 8 && password.length <= 128);

  async function copy(): Promise<void> {
    try {
      await navigator.clipboard.writeText(generated);
      showToast(t('usr.pw.copied'));
    } catch {
      // the clipboard may be blocked on a plain http page; the password is still shown for a manual copy
    }
  }

  async function save(): Promise<void> {
    if (busy || !valid) return;
    busy = true;
    error = null;
    try {
      await api.hostUserPassword(serverId, user.name, password);
      showToast(t('usr.pw.done', undefined, { user: user.name }));
      onclose();
    } catch (e) {
      error = e instanceof ApiError && e.serverMessage ? e.serverMessage : errorText(e, (k, v) => t(k, undefined, v));
    } finally {
      busy = false;
    }
  }
</script>

<Modal title={$tr('usr.pw.title', { user: user.name })} labelId="usr-pw" {onclose}>
  <div class="stack">
    <div class="opts">
      <label class="opt" class:on={mode === 'generate'}><input type="radio" bind:group={mode} value="generate" /><span>{$tr('usr.pw.generate')}<small class="muted">{$tr('usr.pw.generateHint')}</small></span></label>
      <label class="opt" class:on={mode === 'manual'}><input type="radio" bind:group={mode} value="manual" /><span>{$tr('usr.pw.manual')}</span></label>
    </div>
    {#if mode === 'generate'}
      <div class="row">
        <input class="input mono grow" readonly value={generated} aria-label={$tr('usr.pw.generated')} />
        <button class="btn" type="button" onclick={() => void copy()}>{$tr('common.copy')}</button>
        <button class="btn" type="button" onclick={() => (generated = genPassword())}>{$tr('usr.pw.again')}</button>
      </div>
      <p class="muted small">{$tr('usr.pw.once')}</p>
    {:else}
      <input class="input" type="password" autocomplete="new-password" bind:value={manual} aria-label={$tr('usr.pw.new')} />
      <p class="muted small">{$tr('usr.pw.rule')}</p>
    {/if}
    {#if error}<p class="error" role="alert">{error}</p>{/if}
    <div class="row end">
      <button class="btn" type="button" onclick={onclose}>{$tr('common.cancel')}</button>
      <button class="btn primary" type="button" disabled={busy || !valid} onclick={() => void save()}>{$tr('usr.pw.set')}</button>
    </div>
  </div>
</Modal>

<style>
  .opts {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .opt {
    display: flex;
    gap: 10px;
    align-items: flex-start;
    padding: 8px 10px;
    border: 2px solid var(--seg-off);
    cursor: pointer;
  }

  .opt.on {
    border-color: var(--line);
  }

  .opt small {
    display: block;
  }

  .grow {
    flex: 1 1 200px;
    min-width: 0;
  }

  @media (max-width: 720px) {
    .stack > :global(.row.end:last-child) {
      position: sticky;
      bottom: 0;
      z-index: 2;
      flex-wrap: nowrap;
      margin: 0 -12px calc(-12px - var(--safe-b));
      padding: 10px 12px calc(10px + var(--safe-b));
      background: var(--surface);
      border-top: 2px solid var(--line);
    }

    .stack > :global(.row.end:last-child > .btn) {
      flex: 1;
    }
  }
</style>
