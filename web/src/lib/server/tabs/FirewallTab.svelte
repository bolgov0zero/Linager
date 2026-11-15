<script lang="ts">
  import { untrack } from 'svelte';
  import Select from '../../ui/Select.svelte';
  import { get } from 'svelte/store';
  import {
    api,
    ApiError,
    type ApplyWarning,
    type FirewallDraft,
    type FirewallInfo,
    type FwChain,
    type FwSet,
    type ServerDetail,
  } from '../../../api';
  import { locale, t, tr as trs } from '../../../i18n';
  import Modal from '../../Modal.svelte';
  import { showToast } from '../../toast';
  import { applyFirewall } from './fw-api';
  import FwPorts from './FwPorts.svelte';
  import FwSetsModal from './FwSetsModal.svelte';
  import SourceInput from './SourceInput.svelte';
  import { buildRule, normalizeRule, type ListRef, type RuleAction, type RuleProto } from './fw-rule';

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

  // "Добавить правило" form.
  let addChain = $state<ChainName>('input');
  let addAction = $state<RuleAction>('accept');
  let addProto = $state<RuleProto>('tcp');
  let addPort = $state('');
  let addSource = $state('');
  let addComment = $state('');
  let formError = $state<string | null>(null);
  let rawOpen = $state(false);
  let rawText = $state('');
  let rawError = $state<string | null>(null);
  let addBusy = $state(false);

  let showRuleset = $state(false);
  let activeChain = $state<ChainName>('input');
  let setsOpen = $state(false);

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
  const shown = $derived<FirewallDraft | null>(info ? (draft ?? fromInfo(info.chains, info.sets)) : null);
  const shownSets = $derived<FwSet[]>(shown?.sets ?? []);
  const listRefs = $derived<ListRef[]>(shownSets.map((x) => ({ name: x.name, family: x.family })));
  const allRuleTexts = $derived((shown?.chains ?? []).flatMap((c) => c.rules.map((r) => r.text)));

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
    const wasSets = info.sets ?? [];
    const nowSets = draft.sets ?? wasSets;
    for (const set of nowSets) {
      const before = wasSets.find((x) => x.name === set.name);
      if (!before) {
        out.push({ key: 'fw.change.setAdded', vars: { name: set.name } });
        continue;
      }
      const added = set.elements.filter((e) => !before.elements.includes(e)).length;
      const removed = before.elements.filter((e) => !set.elements.includes(e)).length;
      if (added || removed) out.push({ key: 'fw.change.setChanged', vars: { name: set.name, added: String(added), removed: String(removed) } });
    }
    for (const set of wasSets) {
      if (!nowSets.some((x) => x.name === set.name)) out.push({ key: 'fw.change.setRemoved', vars: { name: set.name } });
    }
    return out;
  });

  function copySets(sets: FwSet[] | undefined): FwSet[] {
    return (sets ?? []).map((x) => ({ name: x.name, family: x.family, elements: [...x.elements] }));
  }

  function fromInfo(chains: FwChain[], sets: FwSet[] | undefined): FirewallDraft {
    return {
      sets: copySets(sets),
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
      // an older draft has no lists: keep the ones that exist on the host
      sets: copySets(source.sets ?? info?.sets),
      chains: source.chains.map((c) => ({
        name: c.name,
        policy: c.policy,
        rules: c.rules.map((r) => ({ text: r.text })),
      })),
    };
  }

  function base(): FirewallDraft {
    return draft ?? fromInfo(info?.chains ?? [], info?.sets);
  }

  function mapChain(d: FirewallDraft, name: ChainName, fn: (c: FirewallDraft['chains'][number]) => FirewallDraft['chains'][number]): FirewallDraft {
    return { ...d, chains: d.chains.map((c) => (c.name === name ? fn(c) : c)) };
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

  // Counters are shown only for a draft rule that matches a live rule's text.
  function countersText(name: ChainName, text: string): { packets: string; bytes: string } {
    const live = countersOf(name, text);
    return {
      packets: live?.packets === undefined ? '—' : String(live.packets),
      bytes: live?.bytes === undefined ? '—' : fmtBytes(live.bytes),
    };
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
      formError = null;
      rawError = null;
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

  // Validates the rule against the server draft and appends it to the chain. Returns an error text, or null on success.
  async function appendRule(name: ChainName, text: string): Promise<string | null> {
    const sid = serverId;
    const next = mapChain(base(), name, (c) => ({ ...c, rules: [...c.rules, { text }] }));
    addBusy = true;
    try {
      const res = await api.firewallValidate(sid, next);
      if (!res.ok) return res.error || tx('fw.add.invalid');
      if (sid !== serverId) return null;
      commit(next);
      return null;
    } catch (e) {
      return errorText(e);
    } finally {
      addBusy = false;
    }
  }

  async function addFromForm(): Promise<void> {
    if (addBusy) return;
    const built = buildRule({
      proto: addProto,
      port: addPort,
      source: addSource,
      action: addAction,
      comment: addComment,
    }, listRefs);
    if (!built.ok) {
      formError = tx(built.error);
      return;
    }
    formError = await appendRule(addChain, built.text);
    if (!formError) {
      addPort = '';
      addSource = '';
      addComment = '';
    }
  }

  function changeSets(next: FwSet[]): void {
    commit({ ...base(), sets: next });
  }

  /** Rules suggested by the ports table: appended to the input chain one after the other. */
  async function addInputRules(texts: string[]): Promise<string | null> {
    for (const text of texts) {
      const err = await appendRule('input', text);
      if (err) return err;
    }
    activeChain = 'input';
    return null;
  }

  async function addRaw(): Promise<void> {
    const text = normalizeRule(rawText);
    if (!text || addBusy) return;
    rawError = await appendRule(addChain, text);
    if (!rawError) rawText = '';
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
  <p class="card error" role="alert">{$trs('fw.loadError')}</p>
{:else if !info}
  <p class="muted">{$trs('common.loading')}</p>
{:else}
  <div class="fw stack">
    {#if info.available}
      <p class="status mono" role="status">{$trs('fw.status.active')}</p>
    {/if}

    {#if info.ufw_active}
      <p class="warn" role="status">{$trs('fw.ufwWarning')}</p>
    {/if}

    {#if pending}
      <section class="pending" class:expired aria-live="polite">
        <h3>{$trs('fw.pending.title')}</h3>
        <p class="muted">{$trs('fw.pending.explain')}</p>
        <div class="countdown" class:expired>
          {#if expired}
            <span>{$trs('fw.pending.expired')}</span>
          {:else}
            <span class="digits">{formatCountdown(countdown)}</span>
          {/if}
        </div>
        {#if connLost}
          <p class="warn">{$trs('fw.pending.lost')}</p>
        {/if}
        <div class="row">
          <button class="btn primary" type="button" disabled={pendingBusy || expired} onclick={confirmPending}>
            {$trs('fw.pending.confirm')}
          </button>
          <button class="btn danger" type="button" disabled={pendingBusy} onclick={rollbackPending}>
            {$trs('fw.pending.rollback')}
          </button>
        </div>
        {#if pendingError}
          <p class="error" role="alert">{pendingError}</p>
        {/if}
      </section>
    {/if}

    {#if draft}
      <section class="draft" aria-live="polite">
        <h3>{$trs('fw.draft.title')}</h3>
        <p class="muted small">{$trs('fw.draft.hint')}</p>
        {#if changes.length}
          <ul class="changes">
            {#each changes as change, i (i)}
              <li class="mono small">{$trs(change.key, change.vars)}</li>
            {/each}
          </ul>
        {/if}
        <div class="row">
          <button class="btn primary" type="button" disabled={applying || hasPending} onclick={() => apply(false)}>
            {$trs('fw.draft.apply')}
          </button>
          <button class="btn" type="button" disabled={applying} onclick={resetDraft}>{$trs('fw.draft.reset')}</button>
          {#if saving}
            <span class="muted small">{$trs('fw.draft.saving')}</span>
          {/if}
        </div>
        {#if saveError || draftError}
          <p class="error" role="alert">{draftError ?? saveError}</p>
        {/if}
      </section>
    {/if}

    {#if !info.available}
      <p class="card">{$trs('fw.notAvailable')}</p>
    {:else}
      <div class="tabsrow">
        <div class="row" role="group" aria-label={$trs('fw.chains.tabs')}>
          {#each CHAIN_NAMES as name (name)}
            {@const count = shown?.chains.find((c) => c.name === name)?.rules.length ?? 0}
            <button
              class="chip"
              type="button"
              aria-pressed={activeChain === name}
              onclick={() => {
                activeChain = name;
                addChain = name;
              }}>{name} <span class="count">{count}</span></button
            >
          {/each}
        </div>
        <button class="btn" type="button" onclick={() => (setsOpen = true)}>
          {$trs('fw.sets.open')} <span class="count">{shownSets.length}</span>
        </button>
      </div>

      {#each CHAIN_NAMES.filter((n) => n === activeChain) as name (name)}
        {@const chain = shown?.chains.find((c) => c.name === name)}
        {#if chain}
          <section class="chain">
            <div class="chain-head">
              <div class="chain-title">
                <span class="chain-name">chain {name}</span>
                <span class="hook mono">{$trs('fw.chain.hook', { chain: name })}</span>
              </div>
              <Select
                size="sm"
                width="200px"
                ariaLabel={$trs('fw.chain.policyLabel')}
                value={chain.policy}
                onchange={(v) => setPolicy(name, v)}
                options={[
                  { value: 'accept' as Policy, label: 'accept' },
                  { value: 'drop' as Policy, label: 'drop' },
                ]}
              />
            </div>

            <div class="tw">
              <table class="tbl">
                <thead>
                  <tr>
                    <th>{$trs('fw.col.rule')}</th>
                    <th class="r">{$trs('fw.col.packets')}</th>
                    <th class="r">{$trs('fw.col.bytes')}</th>
                    <th class="r"></th>
                  </tr>
                </thead>
                <tbody>
                  {#if chain.rules.length === 0}
                    <tr>
                      <td class="empty" colspan="4">{$trs('fw.chain.empty')}</td>
                    </tr>
                  {:else}
                    {#each chain.rules as rule, index (index + ':' + rule.text)}
                      {@const counters = countersText(name, rule.text)}
                      <tr>
                        <td class="rule mono">{rule.text}</td>
                        <td class="r num">{counters.packets}</td>
                        <td class="r num">{counters.bytes}</td>
                        <td class="r acts">
                          <button
                            class="ib"
                            type="button"
                            aria-label={$trs('fw.rule.up')}
                            title={$trs('fw.rule.up')}
                            disabled={index === 0}
                            onclick={() => moveRule(name, index, -1)}>▲</button
                          ><button
                            class="ib"
                            type="button"
                            aria-label={$trs('fw.rule.down')}
                            title={$trs('fw.rule.down')}
                            disabled={index === chain.rules.length - 1}
                            onclick={() => moveRule(name, index, 1)}>▼</button
                          ><button
                            class="ib del"
                            type="button"
                            aria-label={$trs('fw.rule.delete')}
                            title={$trs('fw.rule.delete')}
                            onclick={() => removeRule(name, index)}>×</button
                          >
                        </td>
                      </tr>
                    {/each}
                  {/if}
                </tbody>
              </table>
            </div>
          </section>
        {/if}
      {/each}

      <section class="add-block">
        <h3 class="sect">{$trs('fw.add.title')}</h3>
        <form
          class="inl"
          onsubmit={(event) => {
            event.preventDefault();
            void addFromForm();
          }}
        >
          <Select size="sm" bind:value={addChain} ariaLabel={$trs('fw.add.chain')} options={CHAIN_NAMES.map((c) => ({ value: c, label: c }))} />
          <Select
            size="sm"
            bind:value={addAction}
            ariaLabel={$trs('fw.add.action')}
            options={(['accept', 'drop', 'reject'] as RuleAction[]).map((a) => ({ value: a, label: a }))}
          />
          <Select
            size="sm"
            bind:value={addProto}
            ariaLabel={$trs('fw.add.proto')}
            options={[
              ...(['tcp', 'udp', 'icmp'] as RuleProto[]).map((p) => ({ value: p, label: p as string })),
              { value: 'any' as RuleProto, label: $trs('fw.add.protoAny') },
            ]}
          />
          <input
            class="ctl w mono"
            type="text"
            autocomplete="off"
            spellcheck="false"
            placeholder={$trs('fw.add.port')}
            aria-label={$trs('fw.add.port')}
            bind:value={addPort}
            oninput={() => (formError = null)}
          />
          <SourceInput
            cls="ctl w mono"
            lists={shownSets}
            placeholder={$trs('fw.add.source')}
            ariaLabel={$trs('fw.add.source')}
            bind:value={addSource}
            oninput={() => (formError = null)}
          />
          <input
            class="ctl grow"
            type="text"
            autocomplete="off"
            placeholder={$trs('fw.add.comment')}
            aria-label={$trs('fw.add.comment')}
            bind:value={addComment}
            oninput={() => (formError = null)}
          />
          <button class="btn primary" type="submit" disabled={addBusy}>
            {addBusy ? $trs('fw.add.checking') : $trs('fw.add.submit')}
          </button>
        </form>
        <p class="errt" aria-live="polite">{formError ?? ''}</p>

        <button class="link" type="button" aria-expanded={rawOpen} onclick={() => (rawOpen = !rawOpen)}>
          {$trs('fw.add.rawToggle')}
        </button>
        {#if rawOpen}
          <form
            class="inl"
            onsubmit={(event) => {
              event.preventDefault();
              void addRaw();
            }}
          >
            <input
              class="ctl grow mono"
              type="text"
              autocomplete="off"
              spellcheck="false"
              placeholder={$trs('fw.add.rawPlaceholder')}
              aria-label={$trs('fw.add.rawToggle')}
              bind:value={rawText}
              oninput={() => (rawError = null)}
            />
            <button class="btn" type="submit" disabled={addBusy || !rawText.trim()}>
              {$trs('fw.add.submit')}
            </button>
          </form>
          <p class="errt" aria-live="polite">{rawError ?? ''}</p>
        {/if}
      </section>
    {/if}

    {#if info.available}
      <FwPorts serverId={serverId} lists={shownSets} onrules={addInputRules} />
    {/if}

    <section class="ruleset-block">
      <div class="ruleset-head">
        <h3 class="sect">{$trs('fw.ruleset.title')}</h3>
        <button class="btn" type="button" aria-expanded={showRuleset} onclick={() => (showRuleset = !showRuleset)}>
          {showRuleset ? $trs('fw.ruleset.hide') : $trs('fw.ruleset.show')}
        </button>
      </div>
      {#if showRuleset}
        <pre class="ruleset mono">{info.ruleset}</pre>
      {/if}
      {#if info.other_tables.length}
        <p class="hint">{$trs('fw.otherTables', { tables: info.other_tables.join(', ') })}</p>
      {/if}
    </section>
  </div>
{/if}

{#if setsOpen}
  <FwSetsModal sets={shownSets} rules={allRuleTexts} onchange={changeSets} onclose={() => (setsOpen = false)} />
{/if}

{#if warnOpen}
  <Modal title={$trs('fw.warn.title')} labelId="fw-warn-title" onclose={closeWarnings}>
    <p>{$trs('fw.warn.explain')}</p>
    <ul class="changes">
      {#each warnings as warning, i (i)}
        <li>{warning.message}</li>
      {/each}
    </ul>
    <div class="row end">
      <button class="btn" type="button" onclick={closeWarnings}>{$trs('fw.warn.cancel')}</button>
      <button class="btn danger" type="button" disabled={applying} onclick={() => apply(true)}>
        {$trs('fw.warn.apply')}
      </button>
    </div>
  </Modal>
{/if}

<style>
  .fw {
    min-width: 0;
    gap: 20px;
  }

  h3 {
    margin: 0;
    font-size: 18px;
    line-height: 1.2;
  }

  .sect {
    font-family: var(--font-display);
    font-size: 17px;
    font-weight: 600;
  }

  .status {
    margin: 0;
    color: var(--muted);
    font-size: 12px;
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

  .tabsrow {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
  }

  .tabsrow .count {
    margin-left: 4px;
    color: var(--muted);
  }

  .tabsrow :global(.btn),
  .tabsrow :global(.chip) {
    display: inline-flex;
    align-items: center;
    height: 36px;
    padding-block: 0;
  }

  .chain,
  .add-block,
  .ruleset-block {
    display: flex;
    flex-direction: column;
    gap: 6px;
    min-width: 0;
  }

  .chain-head,
  .ruleset-head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    margin-bottom: 4px;
  }

  .chain-title {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: 10px;
  }

  .chain-name {
    font-family: var(--font-display);
    font-size: 17px;
    font-weight: 600;
  }

  .hook {
    color: var(--muted);
    font-size: 12px;
  }

  .tw {
    overflow-x: auto;
    min-width: 0;
  }

  .tbl {
    width: 100%;
    border-collapse: collapse;
    font-size: 12px;
  }

  .tbl th {
    font-family: var(--font-display);
    font-size: 12px;
    font-weight: 400;
    text-transform: uppercase;
    letter-spacing: 0.08em;
    color: var(--muted);
    text-align: left;
    padding: 6px 8px;
    border-bottom: 2px solid var(--line);
    white-space: nowrap;
  }

  .tbl td {
    padding: 7px 8px;
    border-bottom: 2px dashed var(--seg-off);
    vertical-align: middle;
  }

  .tbl .r {
    text-align: right;
  }

  .tbl td.r {
    white-space: nowrap;
  }

  .num {
    font-variant-numeric: tabular-nums;
  }

  .rule {
    font-size: 12px;
    overflow-wrap: anywhere;
    min-width: 0;
  }

  .empty {
    color: var(--muted);
    font-size: 12px;
  }

  .acts {
    width: 1%;
  }

  .ib {
    width: 26px;
    height: 26px;
    padding: 0;
    display: inline-grid;
    place-items: center;
    vertical-align: middle;
    background: var(--bg);
    color: var(--ink);
    border: 2px solid var(--line);
    border-radius: 0;
    cursor: pointer;
    font-family: var(--font-display);
    font-size: 14px;
    line-height: 1;
  }

  .ib + .ib {
    margin-left: 4px;
  }

  .ib:hover:not(:disabled) {
    background: var(--ink);
    color: var(--surface);
  }

  .ib.del:hover:not(:disabled) {
    background: var(--crit);
    border-color: var(--crit);
    color: var(--surface);
  }

  .ib:disabled {
    opacity: 0.4;
    cursor: not-allowed;
  }

  .fw :global(.ctl) {
    background: var(--bg);
    color: var(--ink);
    border: 2px solid var(--line);
    border-radius: 0;
    padding: 5px 8px;
    min-width: 0;
    font: inherit;
    font-size: 13px;
  }

  .fw :global(.ctl)::placeholder {
    color: var(--muted);
  }

  .fw :global(.ctl.w) {
    width: 170px;
  }

  .fw :global(.ctl.grow) {
    flex: 1 1 180px;
  }

  .fw :global(.ctl):focus-visible,
  .link:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }

  .inl {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
  }

  /* One height for every control of the row: the button is the tallest. */
  .inl :global(.ctl),
  .inl :global(.sel),
  .inl :global(.btn) {
    box-sizing: border-box;
    height: 36px;
    padding-block: 0;
  }

  .inl :global(.btn) {
    display: inline-flex;
    align-items: center;
  }

  .errt {
    margin: 0;
    min-height: 16px;
    color: var(--crit);
    font-size: 12px;
  }

  .link {
    align-self: flex-start;
    padding: 0;
    background: none;
    border: 0;
    color: var(--accent);
    font-family: var(--font-display);
    font-size: 13px;
    text-decoration: underline;
    text-underline-offset: 3px;
    cursor: pointer;
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
