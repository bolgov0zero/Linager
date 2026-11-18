<script lang="ts">
  import { onMount } from 'svelte';
  import { api, ApiError, isUnauthorized, type AvailableServer, type ConnectInfo, type Enrollment } from '../../api';
  import { t, tr } from '../../i18n';
  import { showToast } from '../toast';

  let {
    active,
    count = $bindable(0),
    onadded,
    onauthlost,
  }: {
    /** The tab is on screen; the list of the connected servers is only refreshed then. */
    active: boolean;
    /** How many requests wait (the badge of the tab). */
    count?: number;
    /** A server was approved or put on the dashboard. */
    onadded: () => void;
    onauthlost: () => void;
  } = $props();

  const POLL_MS = 5000;
  const KEY_LENGTH = 9; // XXXX-XXXX

  let items = $state<Enrollment[]>([]);
  let available = $state<AvailableServer[]>([]);
  let connect = $state<ConnectInfo | null>(null);
  let loaded = $state(false);
  let keys = $state<Record<string, string>>({});
  let errors = $state<Record<string, string>>({});
  let busy = $state<Record<string, boolean>>({});

  // The agent port is not behind the admin reverse proxy, so the address is the one the panel was opened with
  // (a domain or an IP); the other names the agent-port certificate was issued for are listed as alternatives.
  const connectHost = $derived(location.hostname);
  const connectTarget = $derived(connect ? `${connectHost}:${connect.port}` : '');
  const otherHosts = $derived(
    (connect?.hosts ?? []).filter(
      (h) => h.toLowerCase() !== connectHost.toLowerCase() && h !== 'localhost' && h !== '127.0.0.1' && h !== '::1',
    ),
  );

  /** Uppercase, strip anything but A-Z/0-9, keep 8 chars and insert the dash after 4. */
  function formatKey(raw: string): string {
    const clean = raw.toUpperCase().replace(/[^A-Z0-9]/g, '').slice(0, 8);
    return clean.length > 4 ? `${clean.slice(0, 4)}-${clean.slice(4)}` : clean;
  }

  function onKeyInput(id: string, event: Event): void {
    const input = event.currentTarget as HTMLInputElement;
    const formatted = formatKey(input.value);
    input.value = formatted;
    keys[id] = formatted;
    errors[id] = '';
  }

  async function loadEnrollments(): Promise<void> {
    try {
      items = await api.enrollments();
      count = items.length;
    } catch (e) {
      if (isUnauthorized(e)) onauthlost();
    } finally {
      loaded = true;
    }
  }

  async function loadAvailable(): Promise<void> {
    try {
      available = await api.availableServers();
    } catch (e) {
      if (isUnauthorized(e)) onauthlost();
    }
  }

  async function refresh(): Promise<void> {
    await Promise.all([loadEnrollments(), active ? loadAvailable() : Promise.resolve()]);
  }

  async function loadConnect(): Promise<void> {
    try {
      connect = await api.connectInfo();
    } catch (e) {
      if (isUnauthorized(e)) onauthlost();
    }
  }

  async function copyTarget(): Promise<void> {
    try {
      await navigator.clipboard.writeText(connectTarget);
      showToast(t('common.copied'));
    } catch {
      showToast(t('common.copyFailed'));
    }
  }

  async function approve(item: Enrollment): Promise<void> {
    const key = item.needs_key ? (keys[item.id] ?? '') : '';
    if ((item.needs_key && key.length !== KEY_LENGTH) || busy[item.id]) return;
    busy[item.id] = true;
    errors[item.id] = '';
    try {
      await api.approveEnrollment(item.id, key);
      showToast(t('asv.confirm.approved', undefined, { name: item.hostname }));
      onadded();
      await Promise.all([loadEnrollments(), loadAvailable()]);
    } catch (e) {
      if (isUnauthorized(e)) {
        onauthlost();
        return;
      }
      if (e instanceof ApiError && e.status === 422) {
        errors[item.id] = t('asv.confirm.invalidKey');
      } else if (e instanceof ApiError && e.status === 410) {
        showToast(t('asv.confirm.tooMany'));
        await loadEnrollments();
      } else {
        errors[item.id] = t('common.genericError');
      }
    } finally {
      busy[item.id] = false;
    }
  }

  async function reject(item: Enrollment): Promise<void> {
    if (busy[item.id]) return;
    busy[item.id] = true;
    errors[item.id] = '';
    try {
      await api.rejectEnrollment(item.id);
      showToast(t('asv.confirm.rejected'));
      await loadEnrollments();
    } catch (e) {
      if (isUnauthorized(e)) {
        onauthlost();
        return;
      }
      errors[item.id] = t('common.genericError');
    } finally {
      busy[item.id] = false;
    }
  }

  async function add(server: AvailableServer): Promise<void> {
    if (busy[server.id]) return;
    busy[server.id] = true;
    try {
      await api.dashboardAdd(server.id);
      showToast(t('asv.avail.added', undefined, { name: server.name }));
      onadded();
      await loadAvailable();
    } catch (e) {
      if (isUnauthorized(e)) {
        onauthlost();
        return;
      }
      if (e instanceof ApiError && e.status === 409) {
        await loadAvailable();
      } else {
        showToast(t('common.genericError'));
      }
    } finally {
      busy[server.id] = false;
    }
  }

  // Coming back to the tab shows fresh data at once.
  $effect(() => {
    if (active) void loadAvailable();
  });

  onMount(() => {
    void loadEnrollments();
    void loadConnect();
    const timer = setInterval(() => {
      if (!document.hidden) void refresh();
    }, POLL_MS);
    return () => clearInterval(timer);
  });
</script>

<section class="panel">
  <h3 class="page">{$tr('asv.tab.confirm')}</h3>

  <div class="sec">
    <h4>{$tr('asv.confirm.pending')}</h4>
    {#if !loaded}
      <p class="muted small">{$tr('common.loading')}</p>
    {:else if items.length === 0}
      <p class="muted small">{$tr('asv.confirm.empty')}</p>
    {/if}
    {#each items as item (item.id)}
      <div class="card" class:link={!!item.via}>
        <div class="row between">
          <div class="who inline">
            <strong class="mono">{item.hostname}</strong>
            <span class="muted mono">{item.address}</span>
          </div>
          {#if item.via}
            <span class="badge acc">{$tr('asv.confirm.badgeLink')}</span>
          {:else}
            <span class="badge muted">{$tr('asv.confirm.badgeManual')}</span>
          {/if}
        </div>
        <dl class="kv">
          <dt>{$tr('asv.confirm.system')}</dt>
          <dd>{item.os}{item.agent_version ? ` · ${$tr('asv.confirm.agent', { version: item.agent_version })}` : ''}</dd>
          <dt>{$tr('asv.confirm.from')}</dt>
          <dd class="mono">{item.remote_addr}</dd>
          <dt>{$tr('asv.confirm.fingerprint')}</dt>
          <dd class="mono">{item.key_fingerprint}</dd>
          {#if item.needs_key}
            <dt>{$tr('asv.confirm.code')}</dt>
            <dd><strong class="mono code">{item.verify_code}</strong> <span class="muted">{$tr('asv.confirm.codeHint')}</span></dd>
          {/if}
        </dl>

        {#if item.needs_key}
          <label class="field">
            <span class="label">{$tr('asv.confirm.keyLabel')}</span>
            <input
              class="input mono"
              class:bad={!!errors[item.id]}
              type="text"
              autocomplete="off"
              autocapitalize="characters"
              spellcheck="false"
              maxlength={KEY_LENGTH}
              placeholder={$tr('asv.confirm.keyPlaceholder')}
              value={keys[item.id] ?? ''}
              oninput={(event) => onKeyInput(item.id, event)}
            />
          </label>
        {:else}
          <p class="box info">{$tr('asv.confirm.viaInfo', { via: item.via ?? '' })}</p>
        {/if}

        {#if errors[item.id]}
          <p class="error small" role="alert">{errors[item.id]}</p>
        {/if}

        <div class="row end buttons">
          <button class="btn" type="button" disabled={busy[item.id]} onclick={() => void reject(item)}>{$tr('asv.confirm.reject')}</button>
          <button
            class="btn primary"
            type="button"
            disabled={busy[item.id] || (item.needs_key && (keys[item.id] ?? '').length !== KEY_LENGTH)}
            onclick={() => void approve(item)}>{$tr('asv.confirm.approve')}</button>
        </div>
      </div>
    {/each}
  </div>

  <div class="sec">
    <h4>{$tr('asv.avail.title')}</h4>
    {#if available.length === 0}
      <p class="muted small">{$tr('asv.avail.empty')}</p>
    {/if}
    {#each available as server (server.id)}
      <div class="card">
        <div class="row between nowrap">
          <div class="who">
            <strong class="mono">{server.name}</strong>
            <span class="muted small mono">{server.address} · {server.os}</span>
          </div>
          <button class="btn sm" type="button" disabled={busy[server.id]} onclick={() => void add(server)}>{$tr('asv.avail.add')}</button>
        </div>
      </div>
    {/each}
  </div>

  {#if connect}
    <div class="sec">
      <h4>{$tr('asv.connect.title')}</h4>
      <div class="card">
        <p class="lead">{$tr('asv.connect.hint')}</p>
        <dl class="kv">
          <dt>{$tr('asv.connect.address')}</dt>
          <dd class="mono">{connectHost}</dd>
          <dt>{$tr('asv.connect.port')}</dt>
          <dd class="mono">{connect.port}</dd>
          <dt>{$tr('asv.connect.fingerprint')}</dt>
          <dd class="mono">{connect.fingerprint}</dd>
        </dl>
        {#if otherHosts.length > 0}
          <p class="muted small mono">{$tr('asv.connect.other', { list: otherHosts.join(', ') })}</p>
        {/if}
        <div class="row">
          <button class="btn sm" type="button" onclick={() => void copyTarget()}>{$tr('asv.connect.copy')}</button>
          <span class="muted small mono">{connectTarget}</span>
        </div>
      </div>
    </div>
  {/if}
</section>

<style>
  .who {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .who.inline {
    flex-direction: row;
    flex-wrap: wrap;
    align-items: baseline;
    gap: 2px 7px;
  }

  .nowrap {
    flex-wrap: nowrap;
  }

  .nowrap .btn {
    flex: none;
  }

  .code {
    font-size: 14px;
    letter-spacing: 0.15em;
  }

  @media (max-width: 720px) {
    .buttons {
      flex-direction: column-reverse;
      align-items: stretch;
    }

    .buttons > .btn {
      width: 100%;
    }
  }
</style>
