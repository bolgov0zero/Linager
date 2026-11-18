<script lang="ts">
  import { untrack } from 'svelte';
  import Select from '../../ui/Select.svelte';
  import {
    api,
    ApiError,
    isUnauthorized,
    type ApplyWarning,
    type FieldError,
    type NetChange,
    type NetInterface,
    type NetworkInfo,
    type ServerDetail,
  } from '../../../api';
  import { locale, t, tr, type Vars } from '../../../i18n';
  import Modal from '../../Modal.svelte';
  import PixIcon from '../../ui/PixIcon.svelte';
  import { isPhone } from '../../ui/viewport';
  import { showToast } from '../../toast';
  import { formatBytes } from '../format';
  import NetDnsModal from './NetDnsModal.svelte';
  import { applyNetwork } from './net-api';
  import { changeFromForm, formFromIface, ifaceAsChange, withChange, type NetForm } from './net-draft';
  import './phone-c.css';

  let { server }: { server: ServerDetail } = $props();

  const POLL_MS = 3000;
  const SETTLE_MS = 400;
  const COUNT_MS = 250;
  // Field names the server may use for each form control; anything else is shown as a general error.
  const FIELD_NAMES: Record<string, string[]> = {
    mode: ['mode'],
    address: ['address'],
    gateway: ['gateway'],
    mtu: ['mtu'],
    state: ['state', 'up'],
  };
  const KNOWN_FIELDS = Object.values(FIELD_NAMES).flat();
  const KIND_KEYS: Record<NetInterface['kind'], string> = {
    physical: 'net.kind.physical',
    virtual: 'net.kind.virtual',
    loopback: 'net.kind.loopback',
  };

  const T = $derived($tr);
  const numberLocale = $derived($locale === 'ru' ? 'ru-RU' : 'en-US');
  const serverId = $derived(server.id);

  let info = $state<NetworkInfo | null>(null);
  let loadFailed = $state(false);
  let loadSeq = 0;

  // Draft as this page knows it: synced from the server while no form is open.
  let draft = $state<NetChange[]>([]);
  let form = $state<NetForm | null>(null);
  let errors = $state<FieldError[]>([]);
  let checking = $state(false);
  let saving = $state(false);
  let busy = $state(false);
  let actionError = $state<string | null>(null);
  let warnings = $state<ApplyWarning[] | null>(null);
  let showConfig = $state(false);
  /** Which non-primary interface cards are expanded; all start closed. The primary one is always open. */
  let openMap = $state<Record<string, boolean>>({});
  let showDns = $state(false);

  let nowMs = $state(Date.now());
  // Pending-change bookkeeping for the toast: not rendered, so plain variables.
  let lastPendingId: string | null = null;
  let confirmedId: string | null = null;
  let validateSeq = 0;

  const ifaceList = $derived(info?.interfaces ?? []);
  // The primary interface goes first; the others keep the agent's order.
  const mainIfaces = $derived(ifaceList.filter((i) => i.primary));
  const otherIfaces = $derived(ifaceList.filter((i) => !i.primary));
  const dnsEditable = $derived(info?.dns_editable ?? false);
  const ifaceMap = $derived(new Map<string, NetInterface>(ifaceList.map((i) => [i.name, i])));
  const draftByIface = $derived(new Map<string, NetChange>(draft.map((c) => [c.iface, c])));
  const editable = $derived(info?.editable ?? false);
  const pending = $derived(info?.pending ?? null);
  const pendingId = $derived(pending?.id ?? null);
  const deadline = $derived(pending?.deadline ?? 0);
  const secondsLeft = $derived(Math.max(0, Math.ceil(deadline - nowMs / 1000)));
  const expired = $derived(pending !== null && deadline - nowMs / 1000 <= 0);
  const hasErrors = $derived(errors.length > 0);
  const canApply = $derived(draft.length > 0 && form === null && pending === null && !busy && !saving);
  const sourceKey = $derived(
    info?.backend === 'ifupdown'
      ? 'net.source.ifupdown'
      : info?.backend === 'netplan'
        ? 'net.source.netplan'
        : 'net.source.unknown',
  );
  const ownErrors = $derived.by(() => {
    const f = form;
    if (!f) return [];
    return errors.filter((e) => e.iface === f.iface && !KNOWN_FIELDS.includes(e.field));
  });

  // Polling: restarts only when the server changes, never on other field updates.
  $effect(() => {
    const id = serverId;
    untrack(() => {
      info = null;
      loadFailed = false;
      draft = [];
      form = null;
      errors = [];
      warnings = null;
      actionError = null;
      lastPendingId = null;
      confirmedId = null;
      void load(id);
    });
    const timer = setInterval(() => untrack(() => void load(id)), POLL_MS);
    return () => {
      clearInterval(timer);
      loadSeq++; // discard in-flight responses for the previous server
    };
  });

  // Countdown ticks only while a pending change exists.
  $effect(() => {
    if (pendingId === null) return;
    nowMs = Date.now();
    const timer = setInterval(() => (nowMs = Date.now()), COUNT_MS);
    return () => clearInterval(timer);
  });

  // Live validation of the draft with the open form, debounced.
  $effect(() => {
    const f = form;
    if (!f) {
      errors = [];
      checking = false;
      return;
    }
    const changes = withChange(draft, changeFromForm(f));
    const id = serverId;
    const seq = ++validateSeq;
    checking = true;
    const timer = setTimeout(async () => {
      try {
        const res = await api.networkValidate(id, { changes });
        if (seq === validateSeq) errors = res.errors;
      } catch {
        // keep the previous result; saving reports its own error
      } finally {
        if (seq === validateSeq) checking = false;
      }
    }, SETTLE_MS);
    return () => clearTimeout(timer);
  });

  async function load(id: string) {
    const seq = ++loadSeq;
    try {
      const res = await api.network(id);
      if (seq !== loadSeq) return;
      loadFailed = false;
      info = res;

      if (res.pending) {
        lastPendingId = res.pending.id;
      } else if (lastPendingId !== null) {
        const finished = lastPendingId;
        lastPendingId = null;
        showToast(t(confirmedId === finished ? 'net.toast.confirmed' : 'net.toast.rolledBack'));
        confirmedId = null;
      }

      if (!form && !saving) draft = res.draft?.changes ?? [];
    } catch (e) {
      if (seq !== loadSeq || isUnauthorized(e)) return;
      loadFailed = true;
    }
  }

  function errorText(e: unknown): string {
    if (e instanceof ApiError && e.status === 0) return t('net.error.network');
    if (e instanceof ApiError && e.serverMessage) return e.serverMessage;
    return t('net.error.generic');
  }

  function modeLabel(mode: string): string {
    if (mode === 'static') return T('net.mode.static');
    if (mode === 'dhcp') return T('net.mode.dhcp');
    if (mode === 'manual') return T('net.mode.manual');
    return '—';
  }

  function describe(c: NetChange): string {
    const vars: Vars = {
      mode: modeLabel(c.mode),
      address: c.address || '—',
      gateway: c.gateway || '—',
      mtu: c.mtu,
      state: c.up ? 'UP' : 'DOWN',
    };
    return T('net.summary', vars);
  }

  function fmt(n: number): string {
    return n.toLocaleString(numberLocale);
  }

  function fieldError(name: string): string | null {
    const f = form;
    if (!f) return null;
    const names = FIELD_NAMES[name] ?? [name];
    const hit = errors.find((e) => e.iface === f.iface && names.includes(e.field));
    return hit ? hit.message : null;
  }

  function isOpen(iface: NetInterface, editing: boolean): boolean {
    return iface.primary || editing || (openMap[iface.name] ?? false);
  }

  function toggleIface(iface: NetInterface, editing: boolean) {
    if (iface.primary || editing) return;
    openMap[iface.name] = !isOpen(iface, editing);
  }

  /** The main IPv4 address for a collapsed row. */
  function mainAddr(iface: NetInterface): string {
    const extra = iface.addresses.length - 1;
    return iface.addresses.length > 0 ? iface.addresses[0] + (extra > 0 ? ` +${extra}` : '') : '—';
  }

  function trafficText(iface: NetInterface): string {
    return `${formatBytes(iface.rx_bytes)} / ${formatBytes(iface.tx_bytes)}`;
  }

  function openForm(iface: NetInterface) {
    openMap[iface.name] = true;
    actionError = null;
    form = formFromIface(iface, draftByIface.get(iface.name));
  }

  function closeForm() {
    form = null;
    errors = [];
    actionError = null;
  }

  async function saveDraft() {
    const f = form;
    if (!f || saving || checking || hasErrors) return;
    const changes = withChange(draft, changeFromForm(f));
    saving = true;
    actionError = null;
    try {
      await api.networkSaveDraft(serverId, { changes });
      draft = changes;
      form = null;
      errors = [];
      showToast(t('net.draft.saved'));
    } catch (e) {
      if (e instanceof ApiError && e.status === 422) {
        actionError = t('net.error.invalid');
        try {
          errors = (await api.networkValidate(serverId, { changes })).errors;
        } catch {
          // the 422 already tells the user something is wrong
        }
      } else {
        actionError = errorText(e);
      }
    } finally {
      saving = false;
    }
  }

  async function resetDraft() {
    if (busy) return;
    busy = true;
    actionError = null;
    try {
      await api.networkResetDraft(serverId);
      draft = [];
      closeForm();
      showToast(t('net.draft.reset'));
    } catch (e) {
      actionError = errorText(e);
    } finally {
      busy = false;
    }
  }

  async function apply(confirm: boolean) {
    if (busy) return;
    busy = true;
    actionError = null;
    try {
      const res = await applyNetwork(serverId, confirm);
      if (res.kind === 'warnings') {
        warnings = res.warnings;
        return;
      }
      draft = [];
      await load(serverId);
    } catch (e) {
      actionError = errorText(e);
    } finally {
      busy = false;
    }
  }

  // ---- raw configuration editor (ifupdown only) ----
  let editingConfig = $state(false);
  let configText = $state('');
  let confirmConfig = $state(false);
  let showEffective = $state(false);
  /** netplan: the file being edited and whether it is a new one. */
  let npFile = $state('');
  let npNew = $state(false);
  let newName = $state('');

  const isNetplan = $derived(info?.backend === 'netplan');
  const canEditConfig = $derived(info?.config_editable ?? editable);
  const npFiles = $derived(info?.netplan_files ?? []);
  const npCurrent = $derived(npFiles.find((f) => f.name === npFile) ?? null);
  const originalText = $derived(isNetplan ? (npCurrent?.text ?? '') : (info?.config_preview ?? ''));
  const fileNameOk = $derived(!npNew || /^[A-Za-z0-9][A-Za-z0-9._-]{0,60}\.yaml$/.test(newName));

  /** The file most likely to hold the primary interface; later files override earlier ones, so the last match wins. */
  function defaultNetplanFile(): string {
    const primary = info?.interfaces.find((i) => i.primary)?.name;
    const matching = primary ? npFiles.filter((f) => new RegExp(`\\b${primary}\\b`).test(f.text)) : [];
    return (matching.at(-1) ?? npFiles.at(-1))?.name ?? '';
  }

  function pickFile(name: string) {
    npFile = name;
    npNew = false;
    configText = npFiles.find((f) => f.name === name)?.text ?? '';
  }

  function startNewFile() {
    npNew = true;
    npFile = '';
    newName = t('net.config.newFileDefault');
    configText = 'network:\n  version: 2\n';
  }

  function startConfigEdit() {
    actionError = null;
    editingConfig = true;
    if (isNetplan) {
      if (npFiles.length === 0) startNewFile();
      else pickFile(defaultNetplanFile());
    } else {
      configText = info?.config_preview ?? '';
    }
  }

  async function applyConfig() {
    if (busy) return;
    busy = true;
    actionError = null;
    confirmConfig = false;
    try {
      await api.networkApplyConfig(serverId, configText, isNetplan ? (npNew ? newName : npFile) : undefined);
      editingConfig = false;
      await load(serverId);
    } catch (e) {
      actionError = errorText(e);
    } finally {
      busy = false;
    }
  }

  async function confirmPending() {
    const id = pendingId;
    if (id === null || busy) return;
    busy = true;
    actionError = null;
    try {
      await api.networkConfirm(serverId, id);
      confirmedId = id;
      await load(serverId);
    } catch (e) {
      actionError = errorText(e);
    } finally {
      busy = false;
    }
  }

  async function rollbackPending() {
    const id = pendingId;
    if (id === null || busy) return;
    busy = true;
    actionError = null;
    try {
      await api.networkRollback(serverId, id);
      await load(serverId);
    } catch (e) {
      actionError = errorText(e);
    } finally {
      busy = false;
    }
  }
</script>

{#snippet phoneHead(iface: NetInterface, inDraft: boolean, open: boolean, fixedOpen: boolean)}
  <span class="pc-mn">
    <span class="pc-l1">
      <b class="mono">{iface.name}</b>
      <span class="pc-tag" class:ok={iface.state === 'UP'} class:muted={iface.state !== 'UP'}>{iface.state}</span>
      <span class="pc-tag muted">{$tr(KIND_KEYS[iface.kind])}</span>
      {#if iface.primary}<span class="pc-tag warn">{$tr('net.badge.primary')}</span>{/if}
      {#if inDraft}<span class="pc-tag accent">{$tr('net.badge.inDraft')}</span>{/if}
    </span>
    <span class="pc-l2 ihint mono">{mainAddr(iface)} · {trafficText(iface)}</span>
  </span>
  {#if !fixedOpen}
    <span class="chev" class:up={open}><PixIcon name="chev" /></span>
  {/if}
{/snippet}

{#snippet deskHead(iface: NetInterface, inDraft: boolean, open: boolean, fixedOpen: boolean)}
  <span class="hname">
    <h3 class="mono">{iface.name}</h3>
    <span class="badges">
      <span class="tag" class:up={iface.state === 'UP'} class:down={iface.state === 'DOWN'}>{iface.state}</span>
      <span class="tag">{$tr(KIND_KEYS[iface.kind])}</span>
      {#if iface.primary}
        <span class="tag warn">{$tr('net.badge.primary')}</span>
      {/if}
      {#if inDraft}
        <span class="tag accent">{$tr('net.badge.inDraft')}</span>
      {/if}
    </span>
  </span>
  {#if !open}
    <span class="sum">
      <span class="mono sum-ip">{mainAddr(iface)}</span>
      <span class="mono sum-tr" title={$tr('net.row.traffic')}>{trafficText(iface)}</span>
    </span>
  {/if}
  {#if !fixedOpen}
    <span class="chev" class:up={open}><PixIcon name="chev" /></span>
  {/if}
{/snippet}

{#snippet ifaceCard(iface: NetInterface)}
        {@const change = draftByIface.get(iface.name)}
        {@const isEditing = form !== null && form.iface === iface.name}
        {@const open = isOpen(iface, isEditing)}
        {@const fixedOpen = iface.primary || isEditing}
        <article class="card iface" class:primary={iface.primary} class:open>
          {#if $isPhone}
            {#if fixedOpen}
              <div class="ihead">{@render phoneHead(iface, !!change, open, fixedOpen)}</div>
            {:else}
              <button class="ihead" type="button" aria-expanded={open} onclick={() => toggleIface(iface, isEditing)}>
                {@render phoneHead(iface, !!change, open, fixedOpen)}
              </button>
            {/if}
          {:else if fixedOpen}
            <header class="iface-head">{@render deskHead(iface, !!change, open, fixedOpen)}</header>
          {:else}
            <button class="iface-head" type="button" aria-expanded={open} onclick={() => toggleIface(iface, isEditing)}>
              {@render deskHead(iface, !!change, open, fixedOpen)}
            </button>
          {/if}

          {#if open}
          <div class="ibody">
          <dl class="facts">
            <div class="fact"><dt>{$tr('net.row.mac')}</dt><dd class="mono">{iface.mac || '—'}</dd></div>
            <div class="fact"><dt>{$tr('net.row.mtu')}</dt><dd>{iface.mtu}</dd></div>
            <div class="fact"><dt>{$tr('net.row.mode')}</dt><dd>{modeLabel(iface.mode)}</dd></div>
            <div class="fact"><dt>{$tr('net.row.ipv4')}</dt><dd class="mono">{iface.addresses.join(', ') || '—'}</dd></div>
            <div class="fact"><dt>{$tr('net.row.gateway')}</dt><dd class="mono">{iface.gateway || '—'}</dd></div>
            <div class="fact">
              <dt>{$tr('net.row.speed')}</dt>
              <dd>{iface.speed_mbps > 0 ? $tr('net.speed', { v: iface.speed_mbps }) : '—'}</dd>
            </div>
            <div class="fact">
              <dt>{$tr('net.row.traffic')}</dt>
              <dd class="mono">{formatBytes(iface.rx_bytes)} / {formatBytes(iface.tx_bytes)}</dd>
            </div>
            <div class="fact">
              <dt>{$tr('net.row.packets')}</dt>
              <dd class="mono">{fmt(iface.rx_packets)} / {fmt(iface.tx_packets)}</dd>
            </div>
            <div class="fact">
              <dt>{$tr('net.row.errors')}</dt>
              <dd class="mono">{fmt(iface.rx_errors)} / {fmt(iface.tx_errors)}</dd>
            </div>
            <div class="fact">
              <dt>{$tr('net.row.drops')}</dt>
              <dd class="mono">{fmt(iface.rx_dropped)} / {fmt(iface.tx_dropped)}</dd>
            </div>
          </dl>

          {#if editable && iface.editable && !isEditing}
            <div class="row">
              <button class="btn" type="button" disabled={busy || pending !== null} onclick={() => openForm(iface)}
                >{$tr('net.edit')}</button
              >
            </div>
          {/if}

          {#if !iface.editable}
            <p class="hint">{$tr('net.readonly')}</p>
          {/if}

          {#if isEditing && form}
            <form
              class="iface-form stack"
              onsubmit={(e) => {
                e.preventDefault();
                void saveDraft();
              }}
            >
              <div class="grid2">
                <label class="field">
                  <span class="label">{$tr('net.form.mode')}</span>
                  <Select
                    bind:value={form.mode}
                    options={[
                      { value: 'static', label: $tr('net.option.static') },
                      { value: 'dhcp', label: $tr('net.option.dhcp') },
                    ]}
                  />
                  {#if fieldError('mode')}<p class="error small">{fieldError('mode')}</p>{/if}
                </label>

                <label class="field">
                  <span class="label">{$tr('net.form.state')}</span>
                  <Select
                    bind:value={form.state}
                    options={[
                      { value: 'UP', label: $tr('net.option.up') },
                      { value: 'DOWN', label: $tr('net.option.down') },
                    ]}
                  />
                  {#if fieldError('state')}<p class="error small">{fieldError('state')}</p>{/if}
                </label>

                <label class="field wide">
                  <span class="label">{$tr('net.form.address')}</span>
                  <input
                    class="input mono"
                    type="text"
                    autocomplete="off"
                    spellcheck="false"
                    placeholder={$tr('net.form.addressHint')}
                    disabled={form.mode === 'dhcp'}
                    bind:value={form.address}
                  />
                  {#if fieldError('address')}<p class="error small">{fieldError('address')}</p>{/if}
                </label>

                <label class="field">
                  <span class="label">{$tr('net.form.gateway')}</span>
                  <input
                    class="input mono"
                    type="text"
                    autocomplete="off"
                    spellcheck="false"
                    disabled={form.mode === 'dhcp'}
                    bind:value={form.gateway}
                  />
                  {#if fieldError('gateway')}<p class="error small">{fieldError('gateway')}</p>{/if}
                </label>

                <label class="field">
                  <span class="label">{$tr('net.form.mtu')} <span class="muted">({$tr('net.form.mtuHint')})</span></span>
                  <input class="input mono" type="number" min="68" max="9000" step="1" bind:value={form.mtu} />
                  {#if fieldError('mtu')}<p class="error small">{fieldError('mtu')}</p>{/if}
                </label>
              </div>

              {#each ownErrors as e (e.field + e.message)}
                <p class="error small">{e.message}</p>
              {/each}

              {#if checking}
                <p class="hint">{$tr('net.checking')}</p>
              {/if}

              <div class="row">
                <button class="btn ink" type="submit" disabled={saving || checking || hasErrors}
                  >{$tr('net.draft.save')}</button
                >
                <button class="btn" type="button" disabled={saving} onclick={closeForm}>{$tr('common.cancel')}</button>
              </div>
            </form>
          {/if}
          </div>
          {/if}
        </article>
{/snippet}

{#snippet changeList()}
  <ul class="changes">
    {#each draft as c (c.iface)}
      {@const cur = ifaceMap.get(c.iface)}
      <li>
        <span class="mono">{c.iface}</span>: {describe(c)}
        {#if cur}
          <span class="muted">({$tr('net.draft.was')}: {describe(ifaceAsChange(cur))})</span>
        {/if}
      </li>
    {/each}
  </ul>
{/snippet}

{#snippet configEditor()}
  <p class="small muted">{$tr(isNetplan ? 'net.config.hintNetplan' : 'net.config.hint')}</p>
  {#if isNetplan}
    <div class="row">
      {#if npNew}
        <input
          class="input mono grow"
          maxlength="64"
          placeholder={$tr('net.config.newFileName')}
          aria-label={$tr('net.config.newFileName')}
          bind:value={newName} />
      {:else}
        <Select
          size="sm"
          value={npFile}
          ariaLabel={$tr('net.config.file')}
          options={npFiles.map((f) => ({ value: f.name, label: f.name }))}
          onchange={pickFile} />
      {/if}
      <button class="btn" type="button" disabled={npNew} onclick={startNewFile}>{$tr('net.config.newFile')}</button>
    </div>
    {#if !fileNameOk}
      <p class="error" role="alert">{$tr('net.config.fileInvalid')}</p>
    {/if}
    {#if npCurrent?.cloud_init}
      <p class="banner" role="status">{$tr('net.config.cloudInit')}</p>
    {/if}
  {/if}
  <textarea
    class="input mono config-edit"
    rows="22"
    spellcheck="false"
    aria-label={$tr('net.config.title')}
    bind:value={configText}></textarea>
  {#if actionError}
    <p class="error" role="alert">{actionError}</p>
  {/if}
  <div class="row end">
    <button class="btn" type="button" disabled={busy} onclick={() => (editingConfig = false)}
      >{$tr('net.config.cancel')}</button
    >
    <button
      class="btn primary"
      type="button"
      disabled={busy || !fileNameOk || configText.trim() === '' || configText.trim() === originalText.trim()}
      onclick={() => (confirmConfig = true)}>{$tr('net.config.apply')}</button
    >
  </div>
{/snippet}

<div class="net stack">
  {#if !info}
    {#if loadFailed}
      <div class="card state">
        <p class="error" role="alert">{$tr('net.loadFailed')}</p>
        <div class="row">
          <button class="btn" type="button" onclick={() => void load(serverId)}>{$tr('net.retry')}</button>
        </div>
      </div>
    {:else}
      <p class="muted">{$tr('common.loading')}</p>
    {/if}
  {:else}
    <p class="note small muted">{$tr('net.note', { source: $tr(sourceKey) })}</p>

    {#if !editable}
      <p class="banner" role="status">{$tr('net.netplanNotice')}</p>
    {/if}

    {#if loadFailed && !pending}
      <p class="hint">{$tr('net.loadFailed')}</p>
    {/if}

    {#if pending}
      <section class="pending" aria-labelledby="net-pending-title">
        <h2 id="net-pending-title">{$tr('net.pending.title')}</h2>
        <div class="pending-body">
          <div class="countdown" aria-hidden="true">
            <span class="count">{secondsLeft}</span>
            <span class="unit">{$tr('net.pending.seconds')}</span>
          </div>
          <div class="pending-text stack">
            {#if loadFailed}
              <p>{$tr('net.pending.lost', { n: secondsLeft })}</p>
            {:else if expired}
              <p>{$tr('net.pending.expired')}</p>
            {:else}
              <p>{$tr('net.pending.text')}</p>
            {/if}
            {#if $isPhone}
              <div class="pc-pbar" aria-hidden="true"><i style:width="{Math.min(100, (secondsLeft / 30) * 100)}%"></i></div>
            {/if}
            <div class="row">
              <button
                class="btn primary"
                type="button"
                disabled={busy || loadFailed || expired}
                onclick={confirmPending}>{$tr('net.pending.confirm')}</button
              >
              <button
                class="btn danger"
                type="button"
                disabled={busy || loadFailed || expired}
                onclick={rollbackPending}>{$tr('net.pending.rollback')}</button
              >
            </div>
          </div>
        </div>
      </section>
    {/if}

    {#if draft.length > 0 && !pending}
      <section class="draft card stack" aria-labelledby="net-draft-title">
        <div class="head">
          <h2 id="net-draft-title">{$tr('net.draft.title')}</h2>
          <div class="row">
            <button class="btn primary" type="button" disabled={!canApply} onclick={() => apply(false)}
              >{$tr('net.draft.apply')}</button
            >
            <button class="btn" type="button" disabled={busy || saving} onclick={resetDraft}
              >{$tr('net.draft.discard')}</button
            >
          </div>
        </div>
        {#if $isPhone}
          <details class="dchanges">
            <summary>{$tr('mobC.net.changes', { n: draft.length })}</summary>
            <p class="hint">{$tr('net.draft.banner')}</p>
            {@render changeList()}
          </details>
        {:else}
          <p class="hint">{$tr('net.draft.banner')}</p>
          {@render changeList()}
        {/if}
      </section>
    {/if}

    {#if actionError}
      <p class="error" role="alert">{actionError}</p>
    {/if}

    <div class="ifaces" class:two={!$isPhone && mainIfaces.length > 0 && otherIfaces.length > 0}>
      {#if ifaceList.length === 0}
        <p class="muted">{$tr('net.ifaces.empty')}</p>
      {/if}
      {#if mainIfaces.length > 0}
        <div class="icol">
          {#each mainIfaces as iface (iface.name)}
            {@render ifaceCard(iface)}
          {/each}
        </div>
      {/if}
      {#if otherIfaces.length > 0}
        <div class="icol">
          {#each otherIfaces as iface (iface.name)}
            {@render ifaceCard(iface)}
          {/each}
        </div>
      {/if}
    </div>

    {#if $isPhone}
      <div class="pc-sech">
        <h2>{$tr('net.dns.title')}</h2>
        {#if dnsEditable}
          <button class="btn" type="button" disabled={busy || pending !== null} onclick={() => (showDns = true)}>{$tr('net.edit')}</button>
        {/if}
      </div>
      <ul class="pc-list">
        {#each info.dns.nameservers as ns (ns)}
          <li class="pc-li"><div class="pc-mn"><div class="pc-l1 mono">{ns}</div></div></li>
        {/each}
        {#each info.dns.search as d (d)}
          <li class="pc-li">
            <div class="pc-mn">
              <div class="pc-l1 mono">{d}</div>
              <div class="pc-l2">{$tr('net.dns.search')}</div>
            </div>
          </li>
        {/each}
        {#if info.dns.nameservers.length === 0 && info.dns.search.length === 0}
          <li class="pc-li"><span class="muted">—</span></li>
        {/if}
      </ul>

      <div class="pc-sech"><h2>{$tr('net.routes.title')}</h2></div>
      {#if info.routes.length === 0}
        <p class="muted">{$tr('net.routes.empty')}</p>
      {:else}
        <ul class="pc-list">
          {#each info.routes as r, i (i)}
            <li class="pc-li">
              <div class="pc-mn">
                <div class="pc-l1 mono"><b>{r.dst}</b></div>
                <div class="pc-l2 mono">{r.gateway ? `via ${r.gateway} ` : ''}dev {r.dev}</div>
              </div>
              <span class="pc-r muted">{r.metric} · {r.protocol}</span>
            </li>
          {/each}
        </ul>
      {/if}
    {:else}
    <div class="split">
      <section class="card stack">
        <div class="head">
          <h2>{$tr('net.dns.title')}</h2>
          {#if dnsEditable}
            <button class="btn" type="button" disabled={busy || pending !== null} onclick={() => (showDns = true)}>{$tr('net.edit')}</button>
          {/if}
        </div>
        <dl class="facts">
          <div class="fact">
            <dt>{$tr('net.dns.servers')}</dt>
            <dd class="mono">{info.dns.nameservers.join(', ') || '—'}</dd>
          </div>
          <div class="fact">
            <dt>{$tr('net.dns.search')}</dt>
            <dd class="mono">{info.dns.search.join(', ') || '—'}</dd>
          </div>
        </dl>
      </section>

      <section class="card stack">
        <h2>{$tr('net.routes.title')}</h2>
        {#if info.routes.length === 0}
          <p class="muted">{$tr('net.routes.empty')}</p>
        {:else}
          <div class="table-wrap">
            <table>
              <thead>
                <!-- svelte-ignore component_name_lowercase -->
                <tr>
                  <th>{$tr('net.col.dst')}</th>
                  <th>{$tr('net.col.gateway')}</th>
                  <th>{$tr('net.col.dev')}</th>
                  <th>{$tr('net.col.metric')}</th>
                  <th>{$tr('net.col.proto')}</th>
                </tr>
              </thead>
              <tbody>
                {#each info.routes as r, i (i)}
                  <!-- svelte-ignore component_name_lowercase -->
                  <tr>
                    <td class="mono">{r.dst}</td>
                    <td class="mono">{r.gateway || '—'}</td>
                    <td class="mono">{r.dev}</td>
                    <td>{r.metric}</td>
                    <td>{r.protocol}</td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
        {/if}
      </section>
    </div>
    {/if}

    <section class="card stack psec">
      <div class="head">
        <h2>{$tr('net.config.title')}</h2>
        <div class="row">
          {#if canEditConfig && !editingConfig}
            <button class="btn" type="button" disabled={busy || pending !== null} onclick={startConfigEdit}
              >{$tr('net.config.edit')}</button
            >
          {/if}
          {#if !editingConfig}
            <button class="btn" type="button" onclick={() => (showConfig = !showConfig)}
              >{showConfig ? $tr('net.config.hide') : $tr('net.config.show')}</button
            >
          {/if}
        </div>
      </div>
      {#if editingConfig && !$isPhone}
        {@render configEditor()}
      {:else if showConfig && !editingConfig}
        <pre class="config mono">{info.config_preview}</pre>
      {/if}
      {#if isNetplan}
        <div class="row">
          <button class="btn" type="button" onclick={() => (showEffective = !showEffective)}
            >{showEffective ? $tr('net.config.effectiveHide') : $tr('net.config.effective')}</button
          >
        </div>
        {#if showEffective}
          {#if info.netplan_effective}
            <pre class="config mono">{info.netplan_effective}</pre>
          {:else}
            <p class="small muted">{$tr('net.config.effectiveEmpty')}</p>
          {/if}
        {/if}
      {/if}
    </section>
  {/if}
</div>

{#if editingConfig && $isPhone}
  <Modal wide title={$tr('net.config.title')} labelId="net-cfgedit-title" onclose={() => (editingConfig = false)}>
    {@render configEditor()}
  </Modal>
{/if}

{#if showDns && info}
  <NetDnsModal
    {serverId}
    nameservers={info.dns.nameservers}
    search={info.dns.search}
    onclose={() => (showDns = false)}
    onapplied={() => {
      showDns = false;
      void load(serverId);
    }}
  />
{/if}

{#if confirmConfig}
  <Modal title={$tr('net.config.confirmTitle')} labelId="net-cfg-title" onclose={() => (confirmConfig = false)}>
    <div class="stack">
      <p>{$tr('net.config.confirmText')}</p>
      <div class="row end">
        <button class="btn" type="button" onclick={() => (confirmConfig = false)}>{$tr('common.cancel')}</button>
        <button class="btn danger" type="button" disabled={busy} onclick={() => void applyConfig()}
          >{$tr('net.config.confirmButton')}</button
        >
      </div>
    </div>
  </Modal>
{/if}

{#if warnings}
  <Modal title={$tr('net.warn.title')} labelId="net-warn-title" onclose={() => (warnings = null)}>
    <div class="stack">
      <p>{$tr('net.warn.text')}</p>
      <ul class="warn-list">
        {#each warnings as w, i (i)}
          <li>{w.message}</li>
        {/each}
      </ul>
      <div class="row end">
        <button class="btn" type="button" onclick={() => (warnings = null)}>{$tr('common.cancel')}</button>
        <button
          class="btn danger"
          type="button"
          disabled={busy}
          onclick={() => {
            warnings = null;
            void apply(true);
          }}>{$tr('net.warn.confirm')}</button
        >
      </div>
    </div>
  </Modal>
{/if}

<style>
  .grow {
    flex: 1 1 200px;
    min-width: 0;
  }

  .config-edit {
    width: 100%;
    resize: vertical;
    white-space: pre;
    overflow: auto;
  }

  .net {
    min-width: 0;
  }

  .head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 7px;
  }

  h2 {
    margin: 0;
    font-size: 16px;
    line-height: 1.1;
  }

  h3 {
    margin: 0;
    font-variation-settings: "ELSH" 40;
    font-size: 18px;
    line-height: 1.1;
    overflow-wrap: anywhere;
  }

  .banner {
    margin: 0;
    padding: 7px 11px;
    border: 2px solid var(--warn);
    color: var(--warn);
  }

  .state {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 11px;
  }

  .pending {
    background: var(--surface);
    border: 2px solid var(--warn);
    box-shadow: 4px 4px 0 var(--shade);
    padding: 14px;
    display: flex;
    flex-direction: column;
    gap: 11px;
  }

  .pending h2 {
    color: var(--warn);
  }

  .pending-body {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 14px 22px;
  }

  .countdown {
    display: flex;
    align-items: baseline;
    gap: 7px;
    min-width: 108px;
  }

  .count {
    font-family: var(--font-display);
    font-size: 58px;
    line-height: 1;
    color: var(--warn);
    font-variant-numeric: tabular-nums;
  }

  .unit {
    color: var(--muted);
  }

  .pending-text {
    flex: 1 1 280px;
    min-width: 0;
  }

  .pending-text p {
    margin: 0;
  }

  .draft {
    border-color: var(--accent);
  }

  .changes {
    margin: 0;
    padding-left: 16px;
    display: flex;
    flex-direction: column;
    gap: 4px;
    overflow-wrap: anywhere;
  }

  .ifaces {
    display: flex;
    flex-direction: column;
    gap: 11px;
  }

  .icol {
    display: flex;
    flex-direction: column;
    gap: 11px;
    min-width: 0;
  }

  /* Wide screen: the primary interface is a column of its own, the other interfaces are a column of folded rows beside it. */
  .ifaces.two {
    display: grid;
    grid-template-columns: minmax(0, 306px) minmax(0, 1fr);
    gap: 11px;
    align-items: start;
  }

  @media (max-width: 900px) {
    .ifaces.two {
      grid-template-columns: minmax(0, 1fr);
    }
  }

  .hname {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 4px;
    min-width: 0;
  }

  .hname h3 {
    margin: 0;
  }

  .badges {
    display: flex;
    flex-wrap: wrap;
    gap: 5px;
  }

  .iface {
    display: flex;
    flex-direction: column;
    gap: 11px;
    min-width: 0;
  }

  .iface.primary {
    border-color: var(--warn);
  }

  .ibody {
    display: contents;
  }

  .iface-head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 5px;
  }

  /* A collapsed interface is one clickable line; the open card keeps the same header. */
  button.iface-head {
    flex-wrap: nowrap;
    width: 100%;
    min-height: 29px;
    padding: 0;
    background: none;
    color: var(--ink);
    border: 0;
    font: inherit;
    text-align: left;
    cursor: pointer;
  }

  .sum {
    display: flex;
    flex: 1;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: 4px 16px;
    min-width: 0;
    margin-left: 5px;
  }

  .sum-ip {
    overflow-wrap: anywhere;
  }

  .sum-tr {
    color: var(--muted);
    white-space: nowrap;
  }

  .iface-head .chev {
    margin-left: auto;
  }

  .sum + .chev {
    margin-left: 0;
  }

  .chev {
    display: grid;
    place-items: center;
    flex: none;
    width: 25px;
    height: 25px;
  }

  .chev.up {
    transform: rotate(180deg);
  }

  /* The wide card lays its facts out in columns. */
  @media (min-width: 721px) {
    .iface.open .facts {
      display: grid;
      grid-template-columns: repeat(auto-fill, minmax(min(234px, 100%), 1fr));
      gap: 4px 25px;
    }

    .iface-form {
      max-width: 576px;
    }
  }

  .tag {
    display: inline-block;
    padding: 0 5px;
    border: 2px solid var(--line);
    background: var(--surface);
    font-size: 11px;
    line-height: 1.5;
  }

  .tag.up {
    color: var(--good);
    border-color: var(--good);
  }

  .tag.down {
    color: var(--muted);
  }

  .tag.warn {
    color: var(--warn);
    border-color: var(--warn);
  }

  .tag.accent {
    color: var(--accent);
    border-color: var(--accent);
  }

  .facts {
    margin: 0;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .fact {
    display: flex;
    justify-content: space-between;
    align-items: baseline;
    gap: 11px;
    min-width: 0;
  }

  .fact dt {
    color: var(--muted);
    flex: none;
  }

  .fact dd {
    margin: 0;
    text-align: right;
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .iface-form {
    border-top: 2px solid var(--line);
    padding-top: 11px;
  }

  .grid2 {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 11px;
  }

  .grid2 .wide {
    grid-column: 1 / -1;
  }

  .split {
    display: grid;
    grid-template-columns: minmax(0, 2fr) minmax(0, 3fr);
    align-items: start;
    gap: 11px;
  }

  .table-wrap {
    overflow-x: auto;
  }

  table {
    width: 100%;
    border-collapse: collapse;
  }

  th,
  td {
    padding: 5px 7px;
    text-align: left;
    border-bottom: 1px solid var(--shade);
    vertical-align: top;
  }

  th {
    color: var(--muted);
    font-weight: 400;
    white-space: nowrap;
  }

  td {
    overflow-wrap: anywhere;
  }

  .config {
    margin: 0;
    padding: 11px;
    overflow-x: auto;
    background: var(--ink);
    color: var(--surface);
    border: 2px solid var(--line);
    font-size: 11px;
    line-height: 1.5;
  }

  .warn-list {
    margin: 0;
    padding-left: 16px;
    display: flex;
    flex-direction: column;
    gap: 5px;
    color: var(--warn);
  }

  @media (max-width: 700px) {
    .split,
    .grid2 {
      grid-template-columns: minmax(0, 1fr);
    }

    .count {
      font-size: 43px;
    }
  }

  /* Phone: expandable interface cards, row lists, pinned draft / countdown bar, full-screen config editor. */
  @media (max-width: 720px) {
    .net {
      gap: 11px;
    }

    .iface {
      padding: 0;
      gap: 0;
    }

    .ihead {
      display: flex;
      align-items: center;
      gap: 9px;
      width: 100%;
      min-height: 56px;
      padding: 7px 7px 7px 11px;
      background: none;
      color: var(--ink);
      border: 0;
      font: inherit;
      text-align: left;
      cursor: pointer;
    }

    .ihead .pc-l1 b {
      font-family: var(--font-display);
      font-size: 16px;
      font-weight: 400;
    }

    div.ihead {
      cursor: default;
    }

    .chev {
      display: grid;
      place-items: center;
      flex: none;
      width: 32px;
      height: 32px;
    }

    .chev.up {
      transform: rotate(180deg);
    }

    .ibody {
      display: flex;
      flex-direction: column;
      gap: 9px;
      padding: 4px 11px 11px;
      border-top: 2px dashed var(--seg-off);
    }

    .facts {
      gap: 0;
    }

    .fact {
      padding: 6px 0;
      border-top: 2px dashed var(--seg-off);
      font-size: 12px;
    }

    .fact:first-child {
      border-top: 0;
    }

    .ibody > .row > .btn,
    .iface-form .row > .btn {
      flex: 1;
    }

    .iface-form {
      padding-top: 9px;
    }

    .psec {
      padding: 0;
      background: none;
      border: 0;
      box-shadow: none;
    }

    .psec .head h2 {
      font-size: 16px;
    }

    .psec .head {
      flex-wrap: nowrap;
    }

    .psec .head .row {
      flex-wrap: nowrap;
    }

    .config {
      overflow-x: auto;
    }

    .config-edit {
      flex: none;
      height: max(320px, 55dvh);
      resize: none;
    }

    /* The draft and the countdown stay above the bottom bar at any scroll position. */
    .pending,
    .draft {
      order: 90;
      position: sticky;
      bottom: calc(var(--bar-h) + var(--safe-b));
      z-index: 6;
      /* The negative bottom margin cancels the 16 px page padding: at the end of the scroll the bar still lies on the tab bar. */
      margin: 0 -11px -14px;
      padding: 9px 11px;
      gap: 7px;
      border-width: 2px 0 0;
      box-shadow: 0 -4px 0 var(--shade);
    }

    .pending-body {
      flex-wrap: nowrap;
      align-items: flex-start;
      gap: 11px;
    }

    .countdown {
      min-width: 0;
      flex-direction: column;
      align-items: center;
      gap: 0;
    }

    .count {
      font-size: 32px;
    }

    .unit {
      font-size: 10px;
    }

    .pending-text {
      flex: 1 1 0;
      gap: 7px;
      font-size: 11px;
    }

    .pending .row,
    .draft .row {
      flex-wrap: nowrap;
    }

    .pending .row > .btn,
    .draft .row > .btn {
      flex: 1;
      min-width: 0;
    }

    .draft .head {
      flex-direction: column;
      align-items: stretch;
    }

    .dchanges summary {
      min-height: 29px;
      display: flex;
      align-items: center;
      cursor: pointer;
      font-size: 11px;
    }

    .dchanges {
      font-size: 11px;
    }
  }
</style>
