<script lang="ts">
  import { api, ApiError, type HostUser } from '../../../api';
  import { t, tr } from '../../../i18n';
  import { showToast } from '../../toast';
  import Modal from '../../Modal.svelte';
  import { errorText } from './svc';

  let {
    serverId,
    user,
    onclose,
    ondone,
  }: { serverId: string; user: HostUser; onclose: () => void; ondone: () => void } = $props();

  let removeHome = $state(true);
  let typed = $state('');
  let busy = $state(false);
  let error = $state<string | null>(null);

  async function remove(): Promise<void> {
    if (busy || typed !== user.name) return;
    busy = true;
    error = null;
    try {
      await api.hostUserDelete(serverId, user.name, removeHome);
      showToast(t('usr.del.done', undefined, { user: user.name }));
      ondone();
    } catch (e) {
      error = e instanceof ApiError && e.serverMessage ? e.serverMessage : errorText(e, (k, v) => t(k, undefined, v));
    } finally {
      busy = false;
    }
  }
</script>

<Modal title={$tr('usr.del.title', { user: user.name })} labelId="usr-del" {onclose}>
  <div class="stack">
    <p class="warnbox crit">{$tr('usr.del.irreversible')}</p>
    {#if user.sessions > 0 || user.procs > 0}
      <p class="warnbox warn">{$tr('usr.del.running', { s: user.sessions, p: user.procs })}</p>
    {/if}
    <label class="check">
      <input type="checkbox" bind:checked={removeHome} />
      <span>{$tr('usr.del.home', { home: user.home })}<br /><span class="muted small">{$tr('usr.del.homeHint')}</span></span>
    </label>
    {#if user.keys > 0}<p class="muted small">{$tr('usr.del.keys', { n: user.keys })}</p>{/if}
    <label class="field">
      <span class="label">{$tr('usr.del.type', { user: user.name })}</span>
      <input class="input" bind:value={typed} autocomplete="off" spellcheck="false" />
    </label>
    {#if error}<p class="error" role="alert">{error}</p>{/if}
    <div class="row end">
      <button class="btn" type="button" onclick={onclose}>{$tr('common.cancel')}</button>
      <button class="btn danger" type="button" disabled={busy || typed !== user.name} onclick={() => void remove()}>{$tr('usr.del.do')}</button>
    </div>
  </div>
</Modal>

<style>
  .warnbox {
    margin: 0;
    padding: 8px 10px;
    border: 2px solid currentColor;
    font-size: 13px;
  }

  .crit {
    color: var(--crit);
  }

  .warn {
    color: var(--warn);
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
