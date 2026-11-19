<script lang="ts">
  import PixIcon from '../../ui/PixIcon.svelte';
  import { untrack } from 'svelte';
  import { api, ApiError } from '../../../api';
  import { tr, t } from '../../../i18n';
  import Modal from '../../Modal.svelte';
  import { checkDns, DNS_MAX_SERVERS } from './net-dns';

  let {
    serverId,
    nameservers,
    search,
    onclose,
    onapplied,
  }: {
    serverId: string;
    nameservers: string[];
    search: string[];
    onclose: () => void;
    /** The agent has applied the change; the confirm banner takes over. */
    onapplied: () => void;
  } = $props();

  let rows = $state<string[]>(untrack(() => initialRows()));
  let searchText = $state(untrack(() => search.join(' ')));
  let busy = $state(false);
  let error = $state<string | null>(null);

  function initialRows(): string[] {
    const list = nameservers.slice(0, DNS_MAX_SERVERS);
    return list.length > 0 ? list : [''];
  }

  const check = $derived(checkDns(rows, searchText));
  const same = $derived(
    check.nameservers.join(' ') === nameservers.join(' ') && check.domains.join(' ') === search.join(' '),
  );

  function addRow() {
    if (rows.length < DNS_MAX_SERVERS) rows = [...rows, ''];
  }

  function removeRow(i: number) {
    rows = rows.length > 1 ? rows.filter((_, j) => j !== i) : [''];
  }

  async function submit() {
    if (busy || !check.ok || same) return;
    busy = true;
    error = null;
    try {
      await api.networkApplyDns(serverId, check.nameservers, check.domains);
      onapplied();
    } catch (e) {
      if (e instanceof ApiError && e.status === 0) error = t('net.error.network');
      else if (e instanceof ApiError && e.serverMessage) error = e.serverMessage;
      else error = t('net.error.generic');
    } finally {
      busy = false;
    }
  }
</script>

<Modal title={$tr('net.dns.edit.title')} labelId="net-dns-title" {onclose}>
  <form
    class="stack"
    onsubmit={(e) => {
      e.preventDefault();
      void submit();
    }}
  >
    <div class="field">
      <span class="label">{$tr('net.dns.servers')} <span class="muted">({$tr('net.dns.edit.limit', { n: DNS_MAX_SERVERS })})</span></span>
      {#each rows as _, i (i)}
        <div class="row nsrow">
          <input
            class="input mono grow"
            type="text"
            autocomplete="off"
            spellcheck="false"
            placeholder={$tr('net.dns.edit.placeholder')}
            aria-label={$tr('net.dns.edit.serverN', { n: i + 1 })}
            aria-invalid={check.rows[i] !== null}
            bind:value={rows[i]}
          />
          <button class="btn" type="button" aria-label={$tr('net.dns.edit.remove')} title={$tr('net.dns.edit.remove')} onclick={() => removeRow(i)}><PixIcon name="x" size={14} /></button>
        </div>
        {#if check.rows[i]}<p class="error small">{$tr(check.rows[i])}</p>{/if}
      {/each}
      {#if check.list && rows.some((r) => r.trim() !== '')}<p class="error small">{$tr(check.list)}</p>{/if}
      <div class="row">
        <button class="btn" type="button" disabled={rows.length >= DNS_MAX_SERVERS} onclick={addRow}>{$tr('net.dns.edit.add')}</button>
      </div>
    </div>

    <label class="field">
      <span class="label">{$tr('net.dns.edit.search')} <span class="muted">({$tr('net.dns.edit.searchHint')})</span></span>
      <input
        class="input mono"
        type="text"
        autocomplete="off"
        spellcheck="false"
        placeholder="example.com lan"
        aria-invalid={check.search !== null}
        bind:value={searchText}
      />
      {#if check.search}<p class="error small">{$tr(check.search)}</p>{/if}
    </label>

    <p class="hint">{$tr('net.dns.edit.note')}</p>

    {#if error}<p class="error" role="alert">{error}</p>{/if}

    <div class="row end">
      <button class="btn" type="button" disabled={busy} onclick={onclose}>{$tr('common.cancel')}</button>
      <button class="btn primary" type="submit" disabled={busy || !check.ok || same}>{$tr('net.dns.edit.apply')}</button>
    </div>
  </form>
</Modal>

<style>
  .grow {
    flex: 1 1 auto;
    min-width: 0;
  }

  .nsrow {
    flex-wrap: nowrap;
  }
</style>
