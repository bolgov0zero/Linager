<script lang="ts">
  import { untrack } from 'svelte';
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
  import { showToast } from '../../toast';
  import { formatBytes } from '../format';
  import { applyNetwork } from './net-api';
  import { changeFromForm, formFromIface, ifaceAsChange, withChange, type NetForm } from './net-draft';

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

  let nowMs = $state(Date.now());
  // Pending-change bookkeeping for the toast: not rendered, so plain variables.
  let lastPendingId: string | null = null;
  let confirmedId: string | null = null;
  let validateSeq = 0;

  const ifaceList = $derived(info?.interfaces ?? []);
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

  function openForm(iface: NetInterface) {
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
        <p class="hint">{$tr('net.draft.banner')}</p>
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
      </section>
    {/if}

    {#if actionError}
      <p class="error" role="alert">{actionError}</p>
    {/if}

    <div class="ifaces">
      {#if ifaceList.length === 0}
        <p class="muted">{$tr('net.ifaces.empty')}</p>
      {/if}
      {#each ifaceList as iface (iface.name)}
        {@const change = draftByIface.get(iface.name)}
        {@const isEditing = form !== null && form.iface === iface.name}
        <article class="card iface" class:primary={iface.primary}>
          <header class="iface-head">
            <h3 class="mono">{iface.name}</h3>
            <span class="tag" class:up={iface.state === 'UP'} class:down={iface.state === 'DOWN'}>{iface.state}</span>
            <span class="tag">{$tr(KIND_KEYS[iface.kind])}</span>
            {#if iface.primary}
              <span class="tag warn">{$tr('net.badge.primary')}</span>
            {/if}
            {#if change}
              <span class="tag accent">{$tr('net.badge.inDraft')}</span>
            {/if}
          </header>

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
                  <select class="input" bind:value={form.mode}>
                    <option value="static">{$tr('net.option.static')}</option>
                    <option value="dhcp">{$tr('net.option.dhcp')}</option>
                  </select>
                  {#if fieldError('mode')}<p class="error small">{fieldError('mode')}</p>{/if}
                </label>

                <label class="field">
                  <span class="label">{$tr('net.form.state')}</span>
                  <select class="input" bind:value={form.state}>
                    <option value="UP">{$tr('net.option.up')}</option>
                    <option value="DOWN">{$tr('net.option.down')}</option>
                  </select>
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
        </article>
      {/each}
    </div>

    <div class="split">
      <section class="card stack">
        <h2>{$tr('net.dns.title')}</h2>
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

    <section class="card stack">
      <div class="head">
        <h2>{$tr('net.config.title')}</h2>
        <button class="btn" type="button" onclick={() => (showConfig = !showConfig)}
          >{showConfig ? $tr('net.config.hide') : $tr('net.config.show')}</button
        >
      </div>
      {#if showConfig}
        <pre class="config mono">{info.config_preview}</pre>
      {/if}
    </section>
  {/if}
</div>

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
  .net {
    min-width: 0;
  }

  .head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
  }

  h2 {
    margin: 0;
    font-size: 18px;
    line-height: 1.1;
  }

  h3 {
    margin: 0;
    font-variation-settings: "ELSH" 40;
    font-size: 20px;
    line-height: 1.1;
    overflow-wrap: anywhere;
  }

  .banner {
    margin: 0;
    padding: 8px 12px;
    border: 2px solid var(--warn);
    color: var(--warn);
  }

  .state {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 12px;
  }

  .pending {
    background: var(--surface);
    border: 2px solid var(--warn);
    box-shadow: 4px 4px 0 var(--shade);
    padding: 16px;
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .pending h2 {
    color: var(--warn);
  }

  .pending-body {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 16px 24px;
  }

  .countdown {
    display: flex;
    align-items: baseline;
    gap: 8px;
    min-width: 120px;
  }

  .count {
    font-family: var(--font-display);
    font-size: 64px;
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
    padding-left: 18px;
    display: flex;
    flex-direction: column;
    gap: 4px;
    overflow-wrap: anywhere;
  }

  .ifaces {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(min(320px, 100%), 1fr));
    gap: 12px;
  }

  .iface {
    display: flex;
    flex-direction: column;
    gap: 12px;
    min-width: 0;
  }

  .iface.primary {
    border-color: var(--warn);
  }

  .iface-head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px;
  }

  .tag {
    display: inline-block;
    padding: 0 6px;
    border: 2px solid var(--line);
    background: var(--surface);
    font-size: 12px;
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
    gap: 12px;
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
    padding-top: 12px;
  }

  .grid2 {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 12px;
  }

  .grid2 .wide {
    grid-column: 1 / -1;
  }

  .split {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 12px;
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
    padding: 6px 8px;
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
    padding: 12px;
    overflow-x: auto;
    background: var(--ink);
    color: var(--surface);
    border: 2px solid var(--line);
    font-size: 12px;
    line-height: 1.5;
  }

  .warn-list {
    margin: 0;
    padding-left: 18px;
    display: flex;
    flex-direction: column;
    gap: 6px;
    color: var(--warn);
  }

  @media (max-width: 700px) {
    .split,
    .grid2 {
      grid-template-columns: minmax(0, 1fr);
    }

    .count {
      font-size: 48px;
    }
  }
</style>
