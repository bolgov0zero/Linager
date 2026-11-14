<script lang="ts">
  import { untrack } from 'svelte';
  import { get } from 'svelte/store';
  import {
    api,
    ApiError,
    type ApplyWarning,
    type FirewallDraft,
    type FirewallInfo,
    type FwChain,
    type ServerDetail,
  } from '../../../api';
  import { locale, t, tr } from '../../../i18n';
  import Modal from '../../Modal.svelte';
  import { showToast } from '../../toast';
  import { applyFirewall } from './fw-api';

  type ChainName = FwChain['name'];
  type Policy = FwChain['policy'];
  type Change = { key: string; vars: Record<string, string> };

  const CHAIN_NAMES: ChainName[] = ['input', 'forward', 'output'];
  const POLL_MS = 3000;
  const SAVE_DEBOUNCE_MS = 500;
  const COUNTDOWN_TICK_MS = 250;

  let { server }: { server: ServerDetail } = $props();

  // Primitive, so polling does not restart when other fields of `server` change.
  const serverId = $derived(server.id);

  let info = $state<FirewallInfo | null>(null);
  let loadError = $state<string | null>(null);
  let connLost = $state(false);

  // Local draft. null means "no draft": the view mirrors info.chains.
  let draft = $state<FirewallDraft | null>(null);
  // True while there are edits that are not yet saved to the server draft.
  let dirty = $state(false);
  let saving = $state(false);
  let saveError = $state<string | null>(null);
  let applying = $state(false);
  let draftError = $state<string | null>(null);

  let warnings = $state<ApplyWarning[]>([]);
  let warnOpen = $state(false);

  let addText = $state<Record<ChainName, string>>({ input: '', forward: '', output: '' });
  let addError = $state<Record<ChainName, string | null>>({ input: null, forward: null, output: null });
  let addBusy = $state<ChainName | null>(null);

  let now = $state(Date.now() / 1000);
  let pendingBusy = $state(false);
  let pendingError = $state<string | null>(null);

  // Plain variables: not read by the template, only used by async handlers.
  let saveTimer: ReturnType<typeof setTimeout> | undefined;
  let saveSeq = 0;

  const pending = $derived(info?.pending ?? null);
  const hasPending = $derived(pending !== null);
  const remaining = $derived(pending ? Math.max(0, pending.deadline - now) : 0);
  const countdown = $derived(Math.ceil(remaining));
  const expired = $derived(pending !== null && remaining <= 0);
  const shown = $derived<FirewallDraft | null>(info ? (draft ?? fromChains(info.chains)) : null);

  const changes = $derived.by<Change[]>(() => {
    if (!draft || !info) return [];
    const out: Change[] = [];
    for (const name of CHAIN_NAMES) {
      const before = info.chains.find((c) => c.name === name);
      const after = draft.chains.find((c) => c.name === name);
      if (!after) continue;
      const beforePolicy = before?.policy ?? after.policy;
      if (beforePolicy !== after.policy) {
        out.push({ key: 'fw.change.policy', vars: { chain: name, from: beforePolicy, to: after.policy } });
      }
      const was = before?.rules.map((r) => r.text) ?? [];
      const nextTexts = after.rules.map((r) => r.text);
      const added = nextTexts.filter((x) => !was.includes(x));
      const removed = was.filter((x) => !nextTexts.includes(x));
      for (const rule of added) out.push({ key: 'fw.change.added', vars: { chain: name, rule } });
      for (const rule of removed) out.push({ key: 'fw.change.removed', vars: { chain: name, rule } });
      if (!added.length && !removed.length && was.join('\n') !== nextTexts.join('\n')) {
        out.push({ key: 'fw.change.order', vars: { chain: name } });
      }
    }
    return out;
  });

  function fromChains(chains: FwChain[]): FirewallDraft {
    return {
      chains: CHAIN_NAMES.map((name) => {
        const chain = chains.find((c) => c.name === name);
        return {
          name,
          policy: chain?.policy ?? 'accept',
          rules: (chain?.rules ?? []).map((r) => ({ text: r.text })),
        };
      }),
    };
  }

  function copyDraft(source: FirewallDraft): FirewallDraft {
    return {
      chains: source.chains.map((c) => ({
        name: c.name,
        policy: c.policy,
        rules: c.rules.map((r) => ({ text: r.text })),
      })),
    };
  }

  function base(): FirewallDraft {
    return draft ?? fromChains(info?.chains ?? []);
  }

  function mapChain(d: FirewallDraft, name: ChainName, fn: (c: FirewallDraft['chains'][number]) => FirewallDraft['chains'][number]): FirewallDraft {
    return { chains: d.chains.map((c) => (c.name === name ? fn(c) : c)) };
  }

  function tx(key: string, vars?: Record<string, string | number>): string {
    return t(key, get(locale), vars);
  }

  function errorText(e: unknown): string {
    if (e instanceof ApiError) {
      if (e.status === 0) return tx('fw.error.network');
      if (e.serverMessage) return e.serverMessage;
      return tx('fw.error.http', { status: e.status });
    }
    return tx('fw.error.unknown');
  }

  function countersOf(name: ChainName, text: string) {
    return info?.chains.find((c) => c.name === name)?.rules.find((r) => r.text === text);
  }

  function fmtNum(n: number): string {
    return n.toLocaleString(get(locale) === 'ru' ? 'ru-RU' : 'en-US');
  }

  function fmtBytes(n: number): string {
    const units = ['b', 'kb', 'mb', 'gb', 'tb'];
    let value = n;
    let i = 0;
    while (value >= 1024 && i < units.length - 1) {
      value /= 1024;
      i++;
    }
    const digits = i === 0 ? 0 : 1;
    return `${value.toLocaleString(get(locale) === 'ru' ? 'ru-RU' : 'en-US', { maximumFractionDigits: digits })} ${tx('fw.unit.' + units[i])}`;
  }

  function formatCountdown(seconds: number): string {
    const m = Math.floor(seconds / 60);
    const s = seconds % 60;
    return `${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`;
  }

  // ---- loading and polling ----

  async function load(sid: string): Promise<void> {
    const seqAtStart = saveSeq;
    try {
      const next = await api.firewall(sid);
      if (sid !== serverId) return;
      info = next;
      loadError = null;
      connLost = false;
      // Never overwrite unsaved local edits, and skip the sync if a save finished while this request was in flight.
      if (!dirty && !saving && seqAtStart === saveSeq) {
        draft = next.draft ? copyDraft(next.draft) : null;
      }
    } catch (e) {
      if (sid !== serverId) return;
      connLost = e instanceof ApiError && e.status === 0;
      loadError = errorText(e);
    }
  }

  $effect(() => {
    const sid = serverId;
    let active = true;
    let timer: ReturnType<typeof setTimeout> | undefined;

    untrack(() => {
      info = null;
      draft = null;
      dirty = false;
      saveError = null;
      draftError = null;
      pendingError = null;
      loadError = null;
      connLost = false;
    });

    const tick = async (): Promise<void> => {
      await load(sid);
      if (active) timer = setTimeout(() => void tick(), POLL_MS);
    };
    void tick();

    return () => {
      active = false;
      clearTimeout(timer);
      clearTimeout(saveTimer);
      saveTimer = undefined;
      // Keep unsaved edits when leaving the tab or switching servers.
      untrack(() => {
        if (dirty && draft) void api.firewallSaveDraft(sid, draft).catch(() => undefined);
      });
    };
  });

  // Countdown ticks only while there is a pending change.
  $effect(() => {
    if (!hasPending) return;
    now = Date.now() / 1000;
    const timer = setInterval(() => (now = Date.now() / 1000), COUNTDOWN_TICK_MS);
    return () => clearInterval(timer);
  });

  // ---- editing and draft saving ----

  function scheduleSave(): void {
    clearTimeout(saveTimer);
    saveTimer = setTimeout(() => void save(), SAVE_DEBOUNCE_MS);
  }

  function commit(next: FirewallDraft): void {
    draft = next;
    dirty = true;
    draftError = null;
    scheduleSave();
  }

  async function save(): Promise<void> {
    clearTimeout(saveTimer);
    saveTimer = undefined;
    const snapshot = draft;
    const sid = serverId;
    if (!snapshot) return;
    saving = true;
    try {
      await api.firewallSaveDraft(sid, snapshot);
      if (draft === snapshot) dirty = false;
      saveError = null;
    } catch (e) {
      saveError = errorText(e);
    } finally {
      saving = false;
      saveSeq++;
    }
  }

  function setPolicy(name: ChainName, policy: Policy): void {
    commit(mapChain(base(), name, (c) => ({ ...c, policy })));
  }

  function removeRule(name: ChainName, index: number): void {
    commit(mapChain(base(), name, (c) => ({ ...c, rules: c.rules.filter((_, i) => i !== index) })));
  }

  function moveRule(name: ChainName, index: number, delta: -1 | 1): void {
    commit(
      mapChain(base(), name, (c) => {
        const target = index + delta;
        if (target < 0 || target >= c.rules.length) return c;
        const rules = [...c.rules];
        [rules[index], rules[target]] = [rules[target], rules[index]];
        return { ...c, rules };
      }),
    );
  }

  async function addRule(name: ChainName): Promise<void> {
    const text = addText[name].trim().replace(/\s+/g, ' ');
    if (!text || addBusy) return;
    const sid = serverId;
    const next = mapChain(base(), name, (c) => ({ ...c, rules: [...c.rules, { text }] }));
    addBusy = name;
    addError[name] = null;
    try {
      const res = await api.firewallValidate(sid, next);
      if (!res.ok) {
        addError[name] = res.error || tx('fw.add.invalid');
        return;
      }
      addText[name] = '';
      commit(next);
    } catch (e) {
      addError[name] = errorText(e);
    } finally {
      addBusy = null;
    }
  }

  async function resetDraft(): Promise<void> {
    clearTimeout(saveTimer);
    saveTimer = undefined;
    const sid = serverId;
    draftError = null;
    try {
      await api.firewallResetDraft(sid);
      draft = null;
      dirty = false;
      saveError = null;
      await load(sid);
    } catch (e) {
      draftError = errorText(e);
    }
  }

  // ---- apply, warnings, pending confirmation ----

  async function apply(confirm: boolean): Promise<void> {
    const sid = serverId;
    applying = true;
    draftError = null;
    try {
      if (dirty) await save();
      if (dirty) {
        draftError = saveError ?? tx('fw.error.unknown');
        return;
      }
      const outcome = await applyFirewall(sid, confirm);
      if (outcome.kind === 'warnings') {
        warnings = outcome.warnings;
        warnOpen = true;
        return;
      }
      warnOpen = false;
      warnings = [];
      draft = null;
      dirty = false;
      await load(sid);
    } catch (e) {
      draftError = errorText(e);
    } finally {
      applying = false;
    }
  }

  function closeWarnings(): void {
    warnOpen = false;
    warnings = [];
  }

  async function confirmPending(): Promise<void> {
    const change = pending;
    if (!change) return;
    const sid = serverId;
    pendingBusy = true;
    pendingError = null;
    try {
      await api.firewallConfirm(sid, change.id);
      showToast(tx('fw.toast.confirmed'));
      await load(sid);
    } catch (e) {
      pendingError = errorText(e);
    } finally {
      pendingBusy = false;
    }
  }

  async function rollbackPending(): Promise<void> {
    const change = pending;
    if (!change) return;
    const sid = serverId;
    pendingBusy = true;
    pendingError = null;
    try {
      await api.firewallRollback(sid, change.id);
      showToast(tx('fw.toast.rolledBack'));
      await load(sid);
    } catch (e) {
      pendingError = errorText(e);
    } finally {
      pendingBusy = false;
    }
  }
</script>

{#if loadError && !info}
  <p class="card error" role="alert">{$tr('fw.loadError')}</p>
{:else if !info}
  <p class="muted">{$tr('common.loading')}</p>
{:else}
  <div class="fw stack">
    {#if info.ufw_active}
      <p class="warn" role="status">{$tr('fw.ufwWarning')}</p>
    {/if}

    {#if pending}
      <section class="pending" class:expired aria-live="polite">
        <h3>{$tr('fw.pending.title')}</h3>
        <p class="muted">{$tr('fw.pending.explain')}</p>
        <div class="countdown" class:expired>
          {#if expired}
            <span>{$tr('fw.pending.expired')}</span>
          {:else}
            <span class="digits">{formatCountdown(countdown)}</span>
          {/if}
        </div>
        {#if connLost}
          <p class="warn">{$tr('fw.pending.lost')}</p>
        {/if}
        <div class="row">
          <button class="btn primary" type="button" disabled={pendingBusy || expired} onclick={confirmPending}>
            {$tr('fw.pending.confirm')}
          </button>
          <button class="btn danger" type="button" disabled={pendingBusy} onclick={rollbackPending}>
            {$tr('fw.pending.rollback')}
          </button>
        </div>
        {#if pendingError}
          <p class="error" role="alert">{pendingError}</p>
        {/if}
      </section>
    {/if}

    {#if draft}
      <section class="draft" aria-live="polite">
        <h3>{$tr('fw.draft.title')}</h3>
        <p class="muted small">{$tr('fw.draft.hint')}</p>
        {#if changes.length}
          <ul class="changes">
            {#each changes as change, i (i)}
              <li class="mono small">{$tr(change.key, change.vars)}</li>
            {/each}
          </ul>
        {/if}
        <div class="row">
          <button class="btn primary" type="button" disabled={applying || hasPending} onclick={() => apply(false)}>
            {$tr('fw.draft.apply')}
          </button>
          <button class="btn" type="button" disabled={applying} onclick={resetDraft}>{$tr('fw.draft.reset')}</button>
          {#if saving}
            <span class="muted small">{$tr('fw.draft.saving')}</span>
          {/if}
        </div>
        {#if saveError || draftError}
          <p class="error" role="alert">{draftError ?? saveError}</p>
        {/if}
      </section>
    {/if}

    {#if !info.available}
      <p class="card">{$tr('fw.notAvailable')}</p>
    {:else}
      {#each CHAIN_NAMES as name (name)}
        {@const chain = shown?.chains.find((c) => c.name === name)}
        {#if chain}
          <section class="chain">
            <header class="chain-head">
              <div class="chain-title">
                <h3>chain {name}</h3>
                <span class="muted small mono">{$tr('fw.chain.hook', { chain: name })}</span>
              </div>
              <div class="policy" role="group" aria-label={$tr('fw.chain.policyLabel')}>
                <button
                  class="chip"
                  type="button"
                  aria-pressed={chain.policy === 'accept'}
                  onclick={() => setPolicy(name, 'accept')}>accept</button
                >
                <button
                  class="chip"
                  type="button"
                  aria-pressed={chain.policy === 'drop'}
                  onclick={() => setPolicy(name, 'drop')}>drop</button
                >
              </div>
            </header>

            {#if chain.rules.length === 0}
              <p class="muted small">{$tr('fw.chain.empty')}</p>
            {:else}
              <ol class="rules">
                {#each chain.rules as rule, index (index + ':' + rule.text)}
                  {@const counter = countersOf(name, rule.text)}
                  <li class="rule">
                    <span class="rule-text mono">{rule.text}</span>
                    {#if counter && counter.packets !== undefined && counter.bytes !== undefined}
                      <span class="counters mono small muted"
                        >{$tr('fw.counters', { packets: fmtNum(counter.packets), bytes: fmtBytes(counter.bytes) })}</span
                      >
                    {/if}
                    <span class="rule-actions">
                      <button
                        class="btn small-btn"
                        type="button"
                        aria-label={$tr('fw.rule.up')}
                        title={$tr('fw.rule.up')}
                        disabled={index === 0}
                        onclick={() => moveRule(name, index, -1)}>▲</button
                      >
                      <button
                        class="btn small-btn"
                        type="button"
                        aria-label={$tr('fw.rule.down')}
                        title={$tr('fw.rule.down')}
                        disabled={index === chain.rules.length - 1}
                        onclick={() => moveRule(name, index, 1)}>▼</button
                      >
                      <button
                        class="btn small-btn"
                        type="button"
                        aria-label={$tr('fw.rule.delete')}
                        title={$tr('fw.rule.delete')}
                        onclick={() => removeRule(name, index)}>×</button
                      >
                    </span>
                  </li>
                {/each}
              </ol>
            {/if}

            <form
              class="add"
              onsubmit={(event) => {
                event.preventDefault();
                void addRule(name);
              }}
            >
              <label class="field add-field">
                <span class="label small">{$tr('fw.add.label', { chain: name })}</span>
                <input
                  class="input mono"
                  type="text"
                  autocomplete="off"
                  spellcheck="false"
                  placeholder={$tr('fw.add.placeholder')}
                  bind:value={addText[name]}
                  oninput={() => (addError[name] = null)}
                />
              </label>
              <button class="btn primary" type="submit" disabled={addBusy !== null || !addText[name].trim()}>
                {addBusy === name ? $tr('fw.add.checking') : $tr('fw.add.submit')}
              </button>
              {#if addError[name]}
                <p class="error small add-error" role="alert">{addError[name]}</p>
              {/if}
            </form>
          </section>
        {/if}
      {/each}
    {/if}

    <section class="ruleset-block">
      <h3>{$tr('fw.ruleset.title')}</h3>
      <pre class="ruleset mono">{info.ruleset}</pre>
      {#if info.other_tables.length}
        <p class="muted small">{$tr('fw.otherTables', { tables: info.other_tables.join(', ') })}</p>
      {/if}
    </section>
  </div>
{/if}

{#if warnOpen}
  <Modal title={$tr('fw.warn.title')} labelId="fw-warn-title" onclose={closeWarnings}>
    <p>{$tr('fw.warn.explain')}</p>
    <ul class="changes">
      {#each warnings as warning, i (i)}
        <li>{warning.message}</li>
      {/each}
    </ul>
    <div class="row end">
      <button class="btn" type="button" onclick={closeWarnings}>{$tr('fw.warn.cancel')}</button>
      <button class="btn danger" type="button" disabled={applying} onclick={() => apply(true)}>
        {$tr('fw.warn.apply')}
      </button>
    </div>
  </Modal>
{/if}

<style>
  .fw {
    min-width: 0;
  }

  h3 {
    margin: 0;
    font-size: 18px;
    line-height: 1.2;
  }

  .warn,
  .draft,
  .pending {
    margin: 0;
    padding: 12px;
    background: var(--surface);
    border: 2px solid var(--warn);
    box-shadow: 3px 3px 0 var(--shade);
  }

  .warn {
    color: var(--warn);
    box-shadow: none;
  }

  .draft,
  .pending {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  .pending {
    border-width: 3px;
    box-shadow: 4px 4px 0 var(--shade);
  }

  .pending.expired {
    border-color: var(--crit);
  }

  .countdown {
    font-family: var(--font-display);
    font-size: 44px;
    line-height: 1;
    color: var(--warn);
  }

  .countdown.expired {
    font-size: 22px;
    color: var(--crit);
  }

  .digits {
    font-variant-numeric: tabular-nums;
  }

  .changes {
    margin: 0;
    padding-left: 18px;
    display: flex;
    flex-direction: column;
    gap: 2px;
    overflow-wrap: anywhere;
  }

  .error {
    margin: 0;
    color: var(--crit);
  }

  .chain {
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 12px;
    border: 2px solid var(--line);
    min-width: 0;
  }

  .chain-head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
  }

  .chain-title {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: 8px;
  }

  .policy {
    display: flex;
    gap: 6px;
  }

  .rules {
    margin: 0;
    padding: 0;
    list-style: none;
    display: flex;
    flex-direction: column;
  }

  .rule {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px 12px;
    padding: 8px 0;
    border-bottom: 1px dashed var(--line);
  }

  .rule:last-child {
    border-bottom: 0;
  }

  .rule-text {
    flex: 1 1 240px;
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .counters {
    white-space: nowrap;
  }

  .rule-actions {
    display: flex;
    gap: 4px;
    margin-left: auto;
  }

  .small-btn {
    padding: 2px 8px;
    box-shadow: 2px 2px 0 var(--shade);
  }

  .add {
    display: flex;
    flex-wrap: wrap;
    align-items: flex-end;
    gap: 8px;
  }

  .add-field {
    flex: 1 1 260px;
    min-width: 0;
  }

  .add-error {
    flex-basis: 100%;
  }

  .ruleset-block {
    display: flex;
    flex-direction: column;
    gap: 8px;
    min-width: 0;
  }

  .ruleset {
    margin: 0;
    padding: 12px;
    max-height: 360px;
    overflow: auto;
    background: #14181e;
    color: #e6e9ef;
    border: 2px solid var(--line);
    box-shadow: 3px 3px 0 var(--shade);
    font-size: 12px;
    line-height: 1.5;
    white-space: pre;
  }
</style>
