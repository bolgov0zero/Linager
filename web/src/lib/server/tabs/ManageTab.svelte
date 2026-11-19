<script lang="ts">
  import {
    ApiError,
    assistantApi,
    desktopApi,
    type AssistSas,
    type AssistSession,
    type AssistSettings,
    type DesktopState,
    type ServerDetail,
  } from '../../../api';
  import { locale, t, tr as T } from '../../../i18n';
  import { gotoServerTab, sessionValue, wantAssistSession } from '../../assistant/nav';
  import { setAssistSettings } from '../../assistant/settings';
  import { setRdpOff } from '../rdp';
  import Modal from '../../Modal.svelte';
  import { showToast } from '../../toast';

  let { server, canDesktop = false }: { server: ServerDetail; canDesktop?: boolean } = $props();

  const serverId = $derived(server.id);
  const serverName = $derived(server.name);
  const online = $derived(server.online);

  const PRESETS: Record<'eco' | 'normal' | 'hq', AssistSettings> = {
    eco: { colors: '256', fps: 8, mbit: 1, nolimit: false, auto: true, show_cursor: false },
    normal: { colors: '24', fps: 15, mbit: 4, nolimit: false, auto: true, show_cursor: false },
    hq: { colors: '24', fps: 30, mbit: 12, nolimit: false, auto: true, show_cursor: false },
  };
  const PRESET_IDS = ['eco', 'normal', 'hq'] as const;
  const COLORS = ['24', '16', '256', 'gray'] as const;

  // ---- the picture ----

  let draft = $state<AssistSettings>({ ...PRESETS.normal });
  let loaded = $state(false);
  let loadError = $state('');
  let saving = $state(false);
  let saveError = $state('');
  /** Set after a successful save: the number of open sessions that took the new values; null while there are edits. */
  let applied = $state<number | null>(null);

  const preset = $derived(
    PRESET_IDS.find((id) => {
      const p = PRESETS[id];
      return p.colors === draft.colors && p.fps === draft.fps && p.nolimit === draft.nolimit && p.auto === draft.auto && (draft.nolimit || p.mbit === draft.mbit);
    }) ?? 'custom',
  );

  const num = (n: number, digits = 1): string => n.toLocaleString($locale === 'ru' ? 'ru-RU' : 'en-GB', { maximumFractionDigits: digits });

  /** A rough guess of the traffic of a busy screen; the real figure is on the line under the picture. */
  const estimate = $derived.by(() => {
    const k = { '24': 1, '16': 0.66, '256': 0.35, gray: 0.3 }[draft.colors];
    return Math.min(draft.nolimit ? 99 : draft.mbit, Math.round(draft.fps * 0.55 * k * 10) / 10);
  });

  async function loadSettings(): Promise<void> {
    try {
      const got = await assistantApi.settings(serverId);
      draft = { ...got, show_cursor: got.show_cursor === true };
      loadError = '';
      loaded = true;
    } catch (e) {
      loadError = e instanceof ApiError && e.serverMessage ? e.serverMessage : t('common.genericError');
    }
  }

  $effect(() => {
    void serverId;
    loaded = false;
    void loadSettings();
  });

  /** Any edit: the "saved" note goes away. */
  function edit(patch: Partial<AssistSettings>): void {
    draft = { ...draft, ...patch };
    applied = null;
    saveError = '';
  }

  async function save(): Promise<void> {
    saving = true;
    saveError = '';
    try {
      const res = await assistantApi.saveSettings(serverId, draft);
      draft = { ...res.settings, show_cursor: res.settings.show_cursor === true };
      applied = res.applied;
      setAssistSettings(serverId, draft);
    } catch (e) {
      saveError = e instanceof ApiError && e.serverMessage ? e.serverMessage : t('common.genericError');
    } finally {
      saving = false;
    }
  }

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
      {#if loadError}
        <p class="error" role="alert">{loadError}</p>
        <button class="btn" type="button" onclick={() => void loadSettings()}>{$T('srv.retry')}</button>
      {:else if !loaded}
        <p class="muted">{$T('common.loading')}</p>
      {:else}
        <div class="fld">
          <div class="fl">{$T('manage.presets')}<small>{$T('manage.presetsHint')}</small></div>
          <div class="segctl">
            {#each PRESET_IDS as id (id)}
              <button class="chip" type="button" aria-pressed={preset === id} onclick={() => edit({ ...PRESETS[id], show_cursor: draft.show_cursor })}>{$T(`manage.preset.${id}`)}</button>
            {/each}
            <button class="chip" type="button" aria-pressed={preset === 'custom'} disabled>{$T('manage.preset.custom')}</button>
          </div>
        </div>
        <div class="fld">
          <div class="fl">{$T('manage.colors')}<small>{$T('manage.colorsHint')}</small></div>
          <div class="segctl">
            {#each COLORS as c (c)}
              <button class="chip" type="button" aria-pressed={draft.colors === c} onclick={() => edit({ colors: c })}>{$T(`manage.color.${c}`)}</button>
            {/each}
          </div>
        </div>
        <div class="fld">
          <div class="fl">{$T('manage.fps')}<small>{$T('manage.fpsHint')}</small></div>
          <div class="range">
            <input type="range" min="1" max="30" step="1" value={draft.fps} aria-label={$T('manage.fps')} oninput={(e) => edit({ fps: Number(e.currentTarget.value) })} />
            <output>{$T('manage.fpsValue', { n: draft.fps })}</output>
          </div>
        </div>
        <div class="fld">
          <div class="fl">{$T('manage.limit')}<small>{$T('manage.limitHint')}</small></div>
          <div>
            <div class="range">
              <input type="range" min="0.5" max="20" step="0.5" value={draft.mbit} disabled={draft.nolimit} aria-label={$T('manage.limit')} oninput={(e) => edit({ mbit: Number(e.currentTarget.value) })} />
              <output>{draft.nolimit ? $T('manage.noLimitValue') : $T('assist.mbitLimit', { v: num(draft.mbit) })}</output>
            </div>
            <label class="check nl">
              <input type="checkbox" checked={draft.nolimit} onchange={(e) => edit({ nolimit: e.currentTarget.checked })} />
              {$T('manage.noLimit')}
            </label>
          </div>
        </div>
        <div class="fld">
          <div class="fl">{$T('manage.quality')}<small>{$T('manage.qualityHint')}</small></div>
          <label class="check">
            <input type="checkbox" checked={draft.auto} onchange={(e) => edit({ auto: e.currentTarget.checked })} />
            {$T('manage.auto')}
          </label>
        </div>
        <div class="fld">
          <div class="fl">{$T('manage.cursor')}<small>{$T('manage.cursorSub')}</small></div>
          <div>
            <label class="check">
              <input type="checkbox" checked={draft.show_cursor} onchange={(e) => edit({ show_cursor: e.currentTarget.checked })} />
              {$T('manage.cursorShow')}
            </label>
            <p class="small muted note">{$T('manage.cursorHint')}</p>
          </div>
        </div>
        <p class="est">{$T('manage.estimate', { peak: num(estimate), idle: num(0.05, 2) })}</p>
        <div class="savebar">
          <button class="btn primary" type="button" disabled={saving} onclick={() => void save()}>{$T('win.save')}</button>
          <button class="btn" type="button" disabled={saving} onclick={() => edit(PRESETS.normal)}>{$T('manage.reset')}</button>
          {#if applied !== null}<span class="saved" role="status">{$T('manage.saved', { n: applied })}</span>{/if}
          {#if saveError}<span class="error" role="alert">{saveError}</span>{/if}
        </div>
      {/if}
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

  .fld {
    display: grid;
    grid-template-columns: 150px minmax(0, 1fr);
    gap: 10px;
    align-items: center;
    padding: 10px 0;
    border-bottom: 1px dashed var(--seg-off);
  }

  .fl {
    font-family: var(--font-display);
  }

  .fl small {
    display: block;
    font-family: var(--font-body);
    color: var(--muted);
    font-size: 11px;
  }

  .segctl {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }

  .chip:disabled {
    cursor: default;
    opacity: 0.55;
  }

  .range {
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .range input[type='range'] {
    flex: 1;
    min-width: 120px;
    height: 20px;
    accent-color: var(--accent);
  }

  .range output {
    min-width: 92px;
    text-align: right;
    font-family: var(--font-display);
    font-size: 14px;
  }

  .nl {
    margin-top: 8px;
  }

  .est {
    margin: 12px 0 0;
    padding: 8px 10px;
    border: 2px dashed var(--line);
    color: var(--muted);
    font-size: 12px;
  }

  .savebar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
    margin-top: 14px;
  }

  .saved {
    color: var(--good);
    font-size: 12px;
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

  @media (max-width: 860px) {
    .fld {
      grid-template-columns: 1fr;
    }
  }
</style>
