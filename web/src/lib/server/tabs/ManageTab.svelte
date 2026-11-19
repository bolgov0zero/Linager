<script lang="ts">
  import {
    ApiError,
    assistantApi,
    desktopApi,
    type AssistSas,
    type AssistSession,
    type DesktopState,
    type ServerDetail,
  } from '../../../api';
  import { t, tr as T } from '../../../i18n';
  import { gotoServerTab, sessionValue, wantAssistSession } from '../../assistant/nav';
  import AssistantQuality from '../../assistant/AssistantQuality.svelte';
  import { setRdpOff } from '../rdp';
  import Modal from '../../Modal.svelte';
  import { showToast } from '../../toast';

  let { server, canDesktop = false }: { server: ServerDetail; canDesktop?: boolean } = $props();

  const serverId = $derived(server.id);
  const serverName = $derived(server.name);
  const online = $derived(server.online);

  // ---- remote desktop (RDP) ----

  let rdp = $state<DesktopState | null>(null);
  let rdpError = $state('');
  let rdpBusy = $state(false);
  let confirmOff = $state(false);

  async function loadRdp(): Promise<void> {
    try {
      rdp = await desktopApi.state(serverId);
      rdpError = '';
    } catch (e) {
      rdp = null;
      rdpError = e instanceof ApiError && e.serverMessage ? e.serverMessage : t('common.genericError');
    }
  }

  $effect(() => {
    void serverId;
    if (canDesktop && online) void loadRdp();
  });

  async function rdpEnable(): Promise<void> {
    rdpBusy = true;
    try {
      await desktopApi.enable(serverId);
      showToast(t('desk.enabled'));
      await loadRdp();
      if (rdp) setRdpOff(serverId, !rdp.enabled || !rdp.supported);
    } catch (e) {
      showToast(e instanceof ApiError && e.serverMessage ? e.serverMessage : t('common.genericError'));
    } finally {
      rdpBusy = false;
    }
  }

  async function rdpDisable(): Promise<void> {
    rdpBusy = true;
    try {
      await desktopApi.disable(serverId);
      confirmOff = false;
      showToast(t('manage.rdpDisabled'));
      await loadRdp();
      if (rdp) setRdpOff(serverId, !rdp.enabled || !rdp.supported);
    } catch (e) {
      confirmOff = false;
      showToast(e instanceof ApiError && e.serverMessage ? e.serverMessage : t('common.genericError'));
    } finally {
      rdpBusy = false;
    }
  }

  // ---- Windows sessions ----

  let sessions = $state<AssistSession[] | null>(null);
  let sessionsError = $state('');
  /** The Ctrl+Alt+Del policy of Windows; null while unknown or when the agent does not report it. */
  let sas = $state<AssistSas | null>(null);
  let sasBusy = $state(false);
  /** The dialog of the Ctrl+Alt+Del policy: the value it would set, null when closed. */
  let confirmSas = $state<boolean | null>(null);

  async function loadSessions(): Promise<void> {
    try {
      const res = await assistantApi.sessions(serverId);
      sessions = res.sessions;
      if (res.sas) sas = res.sas;
      sessionsError = '';
    } catch (e) {
      sessionsError = e instanceof ApiError && e.status === 501 ? t('manage.noModule') : t('common.genericError');
    }
  }

  $effect(() => {
    void serverId;
    if (!online) return;
    void loadSessions();
    const timer = setInterval(() => void loadSessions(), 15000);
    return () => clearInterval(timer);
  });

  async function setSas(enable: boolean): Promise<void> {
    sasBusy = true;
    try {
      sas = await assistantApi.setSas(serverId, enable);
      showToast(t(sas.enabled ? 'manage.sasAllowed' : 'manage.sasDenied'));
    } catch (e) {
      showToast(e instanceof ApiError && e.status === 501 ? t('manage.noModule') : e instanceof ApiError && e.serverMessage ? e.serverMessage : t('common.genericError'));
    } finally {
      confirmSas = null;
      sasBusy = false;
    }
  }

  const rdpSessions = $derived((sessions ?? []).filter((s) => s.type === 'rdp'));
  const rdpActive = $derived(rdpSessions.filter((s) => s.state === 'active').length);

  function stateText(s: AssistSession): string {
    const key = `manage.state.${s.state}`;
    const text = t(key);
    return text === key ? s.state : text;
  }

  function openSession(s: AssistSession): void {
    wantAssistSession(serverId, sessionValue(s));
    gotoServerTab(serverName, 'assistant');
  }

  const rdpOn = $derived(!!rdp && rdp.enabled && rdp.listening);
</script>

<p class="lead muted">{$T('manage.lead')}</p>

<div class="grid2">
  <div class="stack">
    <section class="card">
      <h3 class="t">{$T('manage.picture')}</h3>
      <AssistantQuality {serverId} />
    </section>
  </div>

  <div class="stack">
    {#if canDesktop}
      <section class="card">
        <h3 class="t">{$T('manage.rdp')}</h3>
        {#if !online}
          <p class="muted">{$T('manage.offline')}</p>
        {:else if rdpError}
          <p class="error" role="alert">{rdpError}</p>
        {:else if !rdp}
          <p class="muted">{$T('common.loading')}</p>
        {:else if !rdp.supported}
          <span class="badge warn">{$T('manage.rdpUnavailable')}</span>
          <p class="small muted note">{$T('manage.rdpHome', { edition: rdp.edition })}</p>
        {:else}
          <div class="row">
            <span class="badge" class:ok={rdpOn} class:warn={!rdpOn}><span class="dot"></span>{rdp.enabled ? $T('manage.rdpOn') : $T('manage.rdpOff')}</span>
          </div>
          <dl class="kv">
            <dt>{$T('manage.rdpPort')}</dt>
            <dd>{rdp.port}</dd>
            <dt>{$T('manage.rdpService')}</dt>
            <dd>{rdp.service || '—'}</dd>
            <dt>{$T('manage.rdpListening')}</dt>
            <dd>{rdp.listening ? $T('win.yes') : $T('win.no')}</dd>
            <dt>{$T('manage.rdpSessions')}</dt>
            <dd>{sessions ? $T('manage.rdpSessionsValue', { n: rdpSessions.length, active: rdpActive }) : '—'}</dd>
          </dl>
          <div class="row">
            {#if rdp.enabled}
              <button class="btn danger" type="button" disabled={rdpBusy} onclick={() => (confirmOff = true)}>{$T('manage.rdpDisable')}</button>
            {:else}
              <button class="btn primary" type="button" disabled={rdpBusy} onclick={() => void rdpEnable()}>{$T('manage.rdpEnable')}</button>
            {/if}
          </div>
          <p class="small muted note">{rdp.enabled ? $T('manage.rdpOnNote') : $T('manage.rdpOffNote')}</p>
        {/if}
      </section>
    {/if}

    <section class="card">
      <h3 class="t">{$T('manage.sessions')}</h3>
      {#if !online}
        <p class="muted">{$T('manage.offline')}</p>
      {:else if sessionsError}
        <p class="error" role="alert">{sessionsError}</p>
      {:else if !sessions}
        <p class="muted">{$T('common.loading')}</p>
      {:else if sessions.length === 0}
        <p class="muted">{$T('manage.noSessions')}</p>
      {:else}
        <div class="table-wrap">
          <table class="tbl">
            <thead>
              <tr>
                <th scope="col">{$T('manage.col.id')}</th>
                <th scope="col">{$T('manage.col.user')}</th>
                <th scope="col">{$T('manage.col.type')}</th>
                <th scope="col">{$T('manage.col.state')}</th>
                <th scope="col"><span class="sr">{$T('manage.col.actions')}</span></th>
              </tr>
            </thead>
            <tbody>
              {#each sessions as s (s.id)}
                <tr>
                  <td>{s.id}</td>
                  <td>{s.user || $T('assist.noUser')}</td>
                  <td>{$T(`assist.type.${s.type}`)}{#if s.client}<div class="muted small">{s.client}</div>{/if}</td>
                  <td class:ok={s.state === 'active'} class:warn={s.state !== 'active'}>{stateText(s)}</td>
                  <td class="act">
                    {#if s.screen}
                      <button class="btn sm" type="button" onclick={() => openSession(s)}>{$T('assist.name')}</button>
                    {:else}
                      <span class="muted small">{$T('assist.noScreen')}</span>
                    {/if}
                  </td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {/if}
    </section>

    {#if online && sas}
      <section class="card">
        <h3 class="t">{$T('manage.sas')}</h3>
        <div class="row">
          <span class="badge" class:ok={sas.enabled} class:warn={!sas.enabled}><span class="dot"></span>{sas.enabled ? $T('manage.sasOn') : $T('manage.sasOff')}</span>
        </div>
        <p class="small muted note">{$T('manage.sasNote')}</p>
        <p class="small muted note">{$T('manage.sasPolicy', { n: sas.policy })}</p>
        <div class="row">
          {#if sas.enabled}
            <button class="btn danger" type="button" disabled={sasBusy} onclick={() => (confirmSas = false)}>{$T('manage.sasDeny')}</button>
          {:else}
            <button class="btn primary" type="button" disabled={sasBusy} onclick={() => (confirmSas = true)}>{$T('manage.sasAllow')}</button>
          {/if}
        </div>
      </section>
    {/if}
  </div>
</div>

{#if confirmSas !== null}
  <Modal title={confirmSas ? $T('manage.sasAllowTitle') : $T('manage.sasDenyTitle')} labelId="manage-sas-title" onclose={() => (confirmSas = null)}>
    <p>{confirmSas ? $T('manage.sasAllowText') : $T('manage.sasDenyText')}</p>
    <p class="muted small">{$T('manage.sasNote2')}</p>
    <div class="row end dlg">
      <button class="btn" type="button" onclick={() => (confirmSas = null)}>{$T('win.cancel')}</button>
      <button class="btn danger" type="button" disabled={sasBusy} onclick={() => void setSas(confirmSas === true)}>{confirmSas ? $T('manage.sasAllow') : $T('manage.sasDeny')}</button>
    </div>
  </Modal>
{/if}

{#if confirmOff}
  <Modal title={$T('manage.rdpOffTitle')} labelId="manage-rdp-off-title" onclose={() => (confirmOff = false)}>
    <p>{$T('manage.rdpOffText')}</p>
    <p class="muted small">{$T('manage.rdpOffNote2')}</p>
    <div class="row end dlg">
      <button class="btn" type="button" onclick={() => (confirmOff = false)}>{$T('win.cancel')}</button>
      <button class="btn danger" type="button" disabled={rdpBusy} onclick={() => void rdpDisable()}>{$T('manage.rdpDisable')}</button>
    </div>
  </Modal>
{/if}

<style>
  .lead {
    margin: 0 0 14px;
    max-width: 760px;
  }

  .grid2 {
    display: grid;
    grid-template-columns: minmax(0, 1.25fr) minmax(0, 1fr);
    gap: 14px;
    align-items: start;
  }

  .stack {
    display: flex;
    flex-direction: column;
    gap: 14px;
    min-width: 0;
  }

  .t {
    margin: 0 0 6px;
    font-size: 14px;
    color: var(--muted);
    text-transform: uppercase;
    letter-spacing: 0.06em;
  }

  .badge {
    display: inline-block;
    padding: 0 5px;
    border: 2px solid currentColor;
    font-size: 11px;
    line-height: 1.35;
    white-space: nowrap;
  }

  .ok {
    color: var(--good);
  }

  .warn {
    color: var(--warn);
  }

  .dot {
    display: inline-block;
    width: 7px;
    height: 8px;
    margin-right: 5px;
    background: currentColor;
  }

  .kv {
    display: grid;
    grid-template-columns: auto 1fr;
    gap: 4px 14px;
    margin: 10px 0 14px;
    font-size: 13px;
  }

  .kv dt {
    color: var(--muted);
  }

  .kv dd {
    margin: 0;
  }

  .note {
    margin: 10px 0 0;
  }

  .table-wrap {
    overflow-x: auto;
  }

  .tbl {
    width: 100%;
    border-collapse: collapse;
    font-size: 13px;
  }

  .tbl th {
    padding: 6px 8px;
    text-align: left;
    font-family: var(--font-display);
    font-weight: 400;
    font-size: 12px;
    color: var(--muted);
    border-bottom: 2px solid var(--line);
    white-space: nowrap;
  }

  .tbl td {
    padding: 7px 8px;
    border-bottom: 1px dashed var(--seg-off);
  }

  .act {
    text-align: right;
    white-space: nowrap;
  }

  .sr {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip: rect(0 0 0 0);
  }

  .dlg {
    margin-top: 11px;
  }

  p {
    margin-block: 0 8px;
  }

  @media (max-width: 980px) {
    .grid2 {
      grid-template-columns: 1fr;
    }
  }
</style>
