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
  import PixIcon from '../../ui/PixIcon.svelte';
  import { isPhone } from '../../ui/viewport';
  import { showToast } from '../../toast';
  import { applyFirewall } from './fw-api';
  import Fold from '../../ui/Fold.svelte';
  import FwPorts from './FwPorts.svelte';
  import FwSetsModal from './FwSetsModal.svelte';
  import SourceInput from './SourceInput.svelte';
  import FwLogModal from './FwLogModal.svelte';
  import { canLog, isSystemRule, parseRule, setLogged, type RuleView } from './fw-view';
  import { buildRule, normalizeRule, type ListRef, type RuleAction, type RuleProto } from './fw-rule';
  import './phone-c.css';

  type ChainName = FwChain['name'];
  type Policy = FwChain['policy'];
  type Change = { key: string; vars: Record<string, string> };
  type RuleRow = { chain: ChainName; index: number; text: string; last: number };

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
  /** The "Ports" card is closed by default; the port list is loaded and polled only while it is open. */
  let portsOpen = $state(false);
  let portsMeta = $state<{ n: number; norule: number } | null>(null);
  let activeChain = $state<ChainName | 'all'>('input');
  /** Rules as nft text, not as columns. */
  let asNft = $state(false);
  let setsOpen = $state(false);
  let logOpen = $state(false);
  /** Packets the marked rules blocked in the last hour (null: not loaded or no rule is marked). */
  let logBadge = $state<number | null>(null);

  // Phone: the section under the segmented control, the rule sheet and the "add rule" sheet.
  type Section = 'rules' | 'sets' | 'log';
  let section = $state<Section>('rules');
  let ruleSheet = $state<{ chain: ChainName; index: number } | null>(null);
  let ruleText = $state('');
  let ruleError = $state<string | null>(null);
  let ruleBusy = $state(false);
  let addOpen = $state(false);

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
  const allRows = $derived<RuleRow[]>(
    (shown?.chains ?? []).flatMap((c) => c.rules.map((r, index) => ({ chain: c.name, index, text: r.text, last: c.rules.length - 1 }))),
  );
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
      const was = visible(before?.rules ?? []).map((r) => r.text);
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

  /** The rule for established,related connections is written by the agent itself and never shown or edited. */
  const visible = <T extends { text: string }>(rules: T[]): T[] => rules.filter((r) => !isSystemRule(r.text));

  function fromInfo(chains: FwChain[], sets: FwSet[] | undefined): FirewallDraft {
    return {
      sets: copySets(sets),
      chains: CHAIN_NAMES.map((name) => {
        const chain = chains.find((c) => c.name === name);
        return {
          name,
          policy: chain?.policy ?? 'accept',
          rules: visible(chain?.rules ?? []).map((r) => ({ text: r.text })),
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
        rules: visible(c.rules).map((r) => ({ text: r.text })),
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

  // The closed "Ports" card shows a counter: one request, no polling (the open card polls itself).
  const fwAvailable = $derived(info?.available === true);
  $effect(() => {
    const sid = serverId;
    if (!fwAvailable) return;
    portsMeta = null;
    if (untrack(() => portsOpen)) return;
    let active = true;
    void api
      .firewallPorts(sid)
      .then((res) => {
        if (active && res.available) portsMeta = { n: res.listeners.length, norule: res.listeners.filter((l) => l.firewall === 'open').length };
      })
      .catch(() => undefined);
    return () => {
      active = false;
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
    if (isSystemRule(text)) return tx('fw.add.systemRule');
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

  // ---- the firewall journal ----

  /** Marks a rule for the journal or unmarks it (the rule is rewritten with a log prefix). */
  function toggleLog(chain: ChainName, index: number, on: boolean): void {
    commit(mapChain(base(), chain, (c) => ({ ...c, rules: c.rules.map((r, i) => (i === index ? { text: setLogged(r.text, chain, on) } : r)) })));
  }

  function logDrops(): void {
    let next = base();
    for (const c of next.chains) {
      c.rules.forEach((r, i) => {
        if (/\b(drop|reject)\b/.test(r.text) && canLog(r.text)) next = mapChain(next, c.name, (x) => ({ ...x, rules: x.rules.map((y, j) => (j === i ? { text: setLogged(y.text, c.name, true) } : y)) }));
      });
    }
    commit(next);
  }

  function addToList(list: string, ip: string): string | null {
    const set = shownSets.find((x) => x.name === list);
    if (!set) return tx('fw.log.noList');
    if ((ip.includes(':') ? 'ipv6' : 'ipv4') !== set.family) return tx('fw.log.wrongFamily', { list });
    if (!set.elements.includes(ip)) changeSets(shownSets.map((x) => (x.name === list ? { ...x, elements: [...x.elements, ip] } : x)));
    return null;
  }

  /** A rule that drops the address goes first in input (the draft is applied as usual). */
  async function blockAddress(ip: string): Promise<string | null> {
    const built = buildRule({ proto: 'any', port: '', source: ip, action: 'drop', comment: '' }, listRefs);
    if (!built.ok) return tx(built.error);
    const next = mapChain(base(), 'input', (c) => ({ ...c, rules: [{ text: built.text }, ...c.rules] }));
    try {
      const res = await api.firewallValidate(serverId, next);
      if (!res.ok) return res.error || tx('fw.add.invalid');
    } catch (e) {
      return errorText(e);
    }
    commit(next);
    return null;
  }

  const loggedCount = $derived((shown?.chains ?? []).reduce((n, c) => n + c.rules.filter((r) => parseRule(r.text).logged).length, 0));
  const knownRules = $derived((shown?.chains ?? []).flatMap((c) => c.rules.map((r, index) => ({ chain: c.name, index, text: r.text }))));

  async function loadBadge(sid: string): Promise<void> {
    try {
      const res = await api.firewallLog(sid);
      if (sid !== serverId) return;
      const from = Date.now() / 1000 - 3600;
      logBadge = res.events.filter((e) => e.time >= from && e.action !== 'accept').length;
    } catch {
      /* the badge is optional */
    }
  }

  $effect(() => {
    const sid = serverId;
    if (loggedCount === 0 || logOpen) {
      if (loggedCount === 0) logBadge = null;
      return;
    }
    void loadBadge(sid);
    const timer = setInterval(() => void loadBadge(sid), 60_000);
    return () => clearInterval(timer);
  });

  function sheetRule(): { chain: ChainName; index: number; text: string; last: number } | null {
    const sheet = ruleSheet;
    if (!sheet) return null;
    const chain = shown?.chains.find((c) => c.name === sheet.chain);
    const rule = chain?.rules[sheet.index];
    return rule && chain ? { chain: sheet.chain, index: sheet.index, text: rule.text, last: chain.rules.length - 1 } : null;
  }

  function openRule(row: RuleRow): void {
    ruleSheet = { chain: row.chain, index: row.index };
    ruleText = row.text;
    ruleError = null;
  }

  function moveInSheet(delta: -1 | 1): void {
    const r = sheetRule();
    if (!r) return;
    moveRule(r.chain, r.index, delta);
    ruleSheet = { chain: r.chain, index: r.index + delta };
  }

  function toggleLogInSheet(on: boolean): void {
    const r = sheetRule();
    if (!r) return;
    toggleLog(r.chain, r.index, on);
    ruleText = sheetRule()?.text ?? ruleText;
  }

  function deleteInSheet(): void {
    const r = sheetRule();
    if (!r) return;
    removeRule(r.chain, r.index);
    ruleSheet = null;
  }

  /** Replaces the text of the open rule after the same check an added rule goes through. */
  async function saveSheetRule(): Promise<void> {
    const r = sheetRule();
    if (!r || ruleBusy) return;
    const text = normalizeRule(ruleText);
    if (text === r.text) {
      ruleSheet = null;
      return;
    }
    if (!text) {
      ruleError = tx('fw.add.errEmpty');
      return;
    }
    if (isSystemRule(text)) {
      ruleError = tx('fw.add.systemRule');
      return;
    }
    const next = mapChain(base(), r.chain, (c) => ({ ...c, rules: c.rules.map((x, i) => (i === r.index ? { text } : x)) }));
    ruleBusy = true;
    try {
      const res = await api.firewallValidate(serverId, next);
      if (!res.ok) {
        ruleError = res.error || tx('fw.add.invalid');
        return;
      }
      commit(next);
      ruleSheet = null;
    } catch (e) {
      ruleError = errorText(e);
    } finally {
      ruleBusy = false;
    }
  }

  async function addAndClose(): Promise<void> {
    await addFromForm();
    if (!formError) addOpen = false;
  }

  async function addRawAndClose(): Promise<void> {
    await addRaw();
    if (!rawError && !rawText) addOpen = false;
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

{#snippet changeList()}
  {#if changes.length}
    <ul class="changes">
      {#each changes as change, i (i)}
        <li class="mono small">{$trs(change.key, change.vars)}</li>
      {/each}
    </ul>
  {/if}
{/snippet}

{#snippet portsFold()}
  <Fold
    title={$trs('fw.card.ports')}
    meta={portsMeta ? String(portsMeta.n) : ''}
    tone={portsMeta && portsMeta.norule > 0 ? 'warn' : 'muted'}
    bind:open={portsOpen}
  >
    <FwPorts serverId={serverId} lists={shownSets} onrules={addInputRules} onsummary={(n, norule) => (portsMeta = { n, norule })} />
  </Fold>
{/snippet}

{#if loadError && !info}
  <p class="card error" role="alert">{$trs('fw.loadError')}</p>
{:else if !info}
  <p class="muted">{$trs('common.loading')}</p>
{:else}
  <div class="fw stack">
    {#if info.available && $isPhone}
      <p class="status mono" role="status">{$trs('fw.status.active')}</p>
    {/if}

    {#if info.ufw_active}
      <p class="warn" role="status">{$trs('fw.ufwWarning')}</p>
    {/if}

    <div class="docks">
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
        {#if $isPhone}
          <div class="pc-pbar" aria-hidden="true"><i style:width="{Math.min(100, (remaining / 30) * 100)}%"></i></div>
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
        {#if $isPhone}
          <details class="dchanges">
            <summary>{$trs('mobC.fw.changes', { n: changes.length })}</summary>
            <p class="muted small">{$trs('fw.draft.hint')}</p>
            {@render changeList()}
          </details>
        {:else}
          <p class="muted small">{$trs('fw.draft.hint')}</p>
          {@render changeList()}
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
    </div>

    {#if $isPhone}
      {#if !info.available}
        <p class="card">{$trs('fw.notAvailable')}</p>
      {:else}
        <div class="pc-seg sm" role="group" aria-label={$trs('mobC.fw.sections')}>
          <button type="button" aria-pressed={section === 'rules'} onclick={() => (section = 'rules')}>{$trs('mobC.fw.rules')}</button>
          <button type="button" aria-pressed={section === 'sets'} onclick={() => (section = 'sets')}>{$trs('mobC.fw.sets')}</button>
          <button type="button" aria-pressed={section === 'log'} onclick={() => (section = 'log')}>
            {$trs('fw.log.open')}{#if logBadge !== null && logBadge > 0}<span class="cnt">{logBadge}</span>{/if}
          </button>
        </div>

        {#if section === 'rules'}
          <div class="pc-seg" role="group" aria-label={$trs('fw.chains.tabs')}>
            <button type="button" aria-pressed={activeChain === 'all'} onclick={() => (activeChain = 'all')}>
              {$trs('fw.chains.all')}<span class="cnt">{allRows.length}</span>
            </button>
            {#each CHAIN_NAMES as name (name)}
              {@const count = shown?.chains.find((c) => c.name === name)?.rules.length ?? 0}
              <button
                type="button"
                aria-pressed={activeChain === name}
                onclick={() => {
                  activeChain = name;
                  addChain = name;
                }}>{name}<span class="cnt">{count}</span></button
              >
            {/each}
          </div>

          {#if activeChain !== 'all'}
            {@const chain = shown?.chains.find((c) => c.name === activeChain)}
            {#if chain}
              <label class="field">
                <span class="label">{$trs('fw.chain.policyLabel')}</span>
                <Select
                  ariaLabel={$trs('fw.chain.policyLabel')}
                  value={chain.policy}
                  onchange={(v) => setPolicy(chain.name, v)}
                  options={[
                    { value: 'accept' as Policy, label: 'accept' },
                    { value: 'drop' as Policy, label: 'drop' },
                  ]}
                />
              </label>
            {/if}
          {/if}

          {@const phoneRows =
            activeChain === 'all'
              ? allRows
              : (shown?.chains.find((c) => c.name === activeChain)?.rules ?? []).map((r, index, all) => ({ chain: activeChain as ChainName, index, text: r.text, last: all.length - 1 }))}
          {#if phoneRows.length === 0}
            <p class="muted pc-empty">{$trs('fw.chain.empty')}</p>
          {:else}
            <div class="pc-list">
              {#each phoneRows as row (row.chain + ':' + row.index + ':' + row.text)}
                {@const v = parseRule(row.text)}
                {@const live = countersOf(row.chain, row.text)}
                <button class="pc-li" class:bad={v.action === 'drop'} class:wn={v.action === 'reject'} type="button" onclick={() => openRule(row)}>
                  <span class="pc-n">{row.index + 1}</span>
                  <span class="pc-mn">
                    <span class="pc-l1 mono rtext">{row.text}</span>
                    <span class="pc-l2">
                      {#if activeChain === 'all'}{row.chain} · {/if}{#if v.comment}{v.comment} · {/if}{live?.packets === undefined ? '—' : $trs('mobC.fw.packets', { n: live.packets })}
                    </span>
                  </span>
                  {#if v.logged}<span class="pc-tag warn">{$trs('mobC.fw.logTag')}</span>{/if}
                  <PixIcon name="more" />
                </button>
              {/each}
            </div>
          {/if}
          <p class="small muted lock">{$trs('fw.chain.established')}</p>

          <button class="btn" type="button" onclick={() => (addOpen = true)}>
            <PixIcon name="plus" />&nbsp;{$trs('fw.add.title')}
          </button>
          <button class="btn" type="button" aria-expanded={showRuleset} onclick={() => (showRuleset = !showRuleset)}>
            {$trs('fw.ruleset.title')}: {showRuleset ? $trs('fw.ruleset.hide') : $trs('fw.ruleset.show')}
          </button>
          {#if showRuleset}
            <pre class="ruleset mono">{info.ruleset}</pre>
          {/if}
          {#if info.other_tables.length}
            <p class="hint">{$trs('fw.otherTables', { tables: info.other_tables.join(', ') })}</p>
          {/if}
        {:else if section === 'sets'}
          {#if shownSets.length === 0}
            <p class="muted pc-empty">{$trs('mobC.fw.noSets')}</p>
          {:else}
            <div class="pc-list">
              {#each shownSets as set (set.name)}
                <button class="pc-li" type="button" onclick={() => (setsOpen = true)}>
                  <span class="pc-mn">
                    <span class="pc-l1 mono"><b>@{set.name}</b></span>
                    <span class="pc-l2">{set.family} · {$trs('mobC.fw.elements', { n: set.elements.length })}</span>
                  </span>
                  <PixIcon name="chev" />
                </button>
              {/each}
            </div>
          {/if}
          <button class="btn" type="button" onclick={() => (setsOpen = true)}>{$trs('fw.sets.open')}</button>
        {:else}
          <div class="pc-list">
            <button class="pc-li" type="button" onclick={() => (logOpen = true)}>
              <span class="pc-mn">
                <span class="pc-l1"><b>{$trs('fw.log.title')}</b></span>
                <span class="pc-l2">{$trs('mobC.fw.logMarked', { n: loggedCount })}</span>
              </span>
              {#if logBadge !== null && logBadge > 0}<span class="pc-tag crit">{logBadge}</span>{/if}
              <PixIcon name="chev" />
            </button>
          </div>
          <button class="btn" type="button" onclick={() => (logOpen = true)}>{$trs('fw.log.open')}</button>
        {/if}
        <!-- The ports are a folded block under the rules, like on a desktop. -->
        {#if section === 'rules'}
          {@render portsFold()}
        {/if}
      {/if}
    {:else}
    <section class="card rules">
      <h3 class="cap">{$trs('fw.card.rules')}</h3>
      {#if info.available}
        <p class="status mono" role="status">{$trs('fw.status.active')}</p>
      {:else}
        <p>{$trs('fw.notAvailable')}</p>
      {/if}
      {#if info.available}
      <div class="tabsrow">
        <div class="row" role="group" aria-label={$trs('fw.chains.tabs')}>
          <button class="chip" type="button" aria-pressed={activeChain === 'all'} onclick={() => (activeChain = 'all')}>
            {$trs('fw.chains.all')} <span class="count">{allRows.length}</span>
          </button>
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
        <div class="row">
          <button class="btn" type="button" aria-pressed={asNft} onclick={() => (asNft = !asNft)}>
            {asNft ? $trs('fw.view.columns') : $trs('fw.view.nft')}
          </button>
          <button class="btn" type="button" onclick={() => (setsOpen = true)}>
            {$trs('fw.sets.open')} <span class="count">{shownSets.length}</span>
          </button>
          <button class="btn" type="button" onclick={() => (logOpen = true)}>
            {$trs('fw.log.open')}{#if logBadge !== null && logBadge > 0} <span class="badge crit cnt">{logBadge}</span>{/if}
          </button>
        </div>
      </div>

      {#snippet rulesTable(rows: RuleRow[], showChain: boolean)}
        <div class="tw">
          <table class="tbl">
            <thead>
              <tr>
                {#if asNft}
                  <th>{$trs('fw.col.rule')}</th>
                {:else}
                  {#if showChain}<th>{$trs('fw.col.chain')}</th>{/if}
                  <th>{$trs('fw.col.action')}</th>
                  <th>{$trs('fw.col.proto')}</th>
                  <th>{$trs('fw.col.port')}</th>
                  <th>{$trs('fw.col.source')}</th>
                  <th>{$trs('fw.col.note')}</th>
                {/if}
                <th>{$trs('fw.col.log')}</th>
                <th class="r">{$trs('fw.col.packets')}</th>
                <th class="r">{$trs('fw.col.bytes')}</th>
                <th class="r"></th>
              </tr>
            </thead>
            <tbody>
              {#each rows as row (row.chain + ':' + row.index + ':' + row.text)}
                {@const counters = countersText(row.chain, row.text)}
                {@const v = parseRule(row.text)}
                <tr title={row.text}>
                  {#if asNft}
                    <td class="rule mono">{showChain ? row.chain + ': ' : ''}{row.text}</td>
                  {:else}
                    {#if showChain}<td class="mono">{row.chain}</td>{/if}
                    <td><span class="act {v.action}">{$trs(`fw.act.${v.action}`)}</span></td>
                    <td class="mono">{v.proto ? (v.proto === 'ICMPV6' ? 'ICMPv6' : v.proto) : $trs('fw.any')}</td>
                    <td class="mono port">{v.port || '—'}</td>
                    <td class="mono src">
                      {#if v.source}
                        {v.source}
                      {:else if v.iface}
                        <span class="muted">{$trs('fw.iface', { name: v.iface })}</span>
                      {:else}
                        <span class="muted">{$trs('fw.any')}</span>
                      {/if}
                    </td>
                    <td class="small note">
                      <div class="notebox">
                        {#if v.comment}<span>{v.comment}</span>{/if}
                        {#if v.extra}<span class="muted mono">{v.extra}</span>{/if}
                      </div>
                    </td>
                  {/if}
                  <td>
                    <button
                      class="sw"
                      type="button"
                      aria-pressed={v.logged}
                      disabled={!canLog(row.text)}
                      aria-label={$trs('fw.log.toggle')}
                      title={$trs('fw.log.toggle')}
                      onclick={() => toggleLog(row.chain, row.index, !v.logged)}
                    ></button>
                  </td>
                  <td class="r num">{counters.packets}</td>
                  <td class="r num">{counters.bytes}</td>
                  <td class="r acts">
                    <button class="ib" type="button" aria-label={$trs('fw.rule.up')} title={$trs('fw.rule.up')} disabled={row.index === 0} onclick={() => moveRule(row.chain, row.index, -1)}><PixIcon name="chevron-up" size={14} /></button
                    ><button class="ib" type="button" aria-label={$trs('fw.rule.down')} title={$trs('fw.rule.down')} disabled={row.index === row.last} onclick={() => moveRule(row.chain, row.index, 1)}><PixIcon name="chevron-down" size={14} /></button
                    ><button class="ib del" type="button" aria-label={$trs('fw.rule.delete')} title={$trs('fw.rule.delete')} onclick={() => removeRule(row.chain, row.index)}><PixIcon name="x" size={14} /></button
                    >
                  </td>
                </tr>
              {:else}
                <tr><td class="empty" colspan="10">{$trs('fw.chain.empty')}</td></tr>
              {/each}
            </tbody>
          </table>
        </div>
      {/snippet}

      {#if activeChain === 'all'}
        <section class="chain">
          <div class="chain-head">
            <div class="chain-title">
              <span class="chain-name">{$trs('fw.chains.all')}</span>
              <span class="hook mono">
                {(shown?.chains ?? []).map((c) => `${c.name}: ${c.policy}`).join(' · ')}
              </span>
            </div>
          </div>
          {@render rulesTable(allRows, true)}
          <p class="small muted lock">{$trs('fw.chain.established')}</p>
        </section>
      {:else}
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
              {@render rulesTable(
                chain.rules.map((r, index) => ({ chain: name, index, text: r.text, last: chain.rules.length - 1 })),
                false,
              )}
              <p class="small muted lock">{$trs('fw.chain.established')}</p>
            </section>
          {/if}
        {/each}
      {/if}

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
    </section>

    {#if info.available}
      {@render portsFold()}
    {/if}
    {/if}
  </div>
{/if}

{#if setsOpen}
  <FwSetsModal sets={shownSets} rules={allRuleTexts} onchange={changeSets} onclose={() => (setsOpen = false)} />
{/if}

{#if logOpen}
  <FwLogModal
    {serverId}
    rules={knownRules}
    sets={shownSets}
    onToggle={toggleLog}
    onToggleDrops={logDrops}
    onAddToList={addToList}
    onBlock={blockAddress}
    onOpenRule={(chain) => {
      logOpen = false;
      activeChain = chain;
      addChain = chain;
    }}
    onclose={() => (logOpen = false)}
  />
{/if}

{#if ruleSheet && $isPhone}
  {@const sr = sheetRule()}
  {#if sr}
    {@const sv = parseRule(sr.text)}
    <Modal title={$trs('mobC.fw.ruleTitle', { n: sr.index + 1 })} labelId="fw-rule-title" onclose={() => (ruleSheet = null)}>
      <form
        class="stack"
        onsubmit={(event) => {
          event.preventDefault();
          void saveSheetRule();
        }}
      >
        <label class="field">
          <span class="label">{$trs('mobC.fw.ruleText')} <span class="muted">· {sr.chain}</span></span>
          <input
            class="input mono"
            type="text"
            autocomplete="off"
            autocapitalize="off"
            spellcheck="false"
            bind:value={ruleText}
            oninput={() => (ruleError = null)}
          />
        </label>
        {#if ruleError}<p class="error small" role="alert">{ruleError}</p>{/if}
        <label class="check">
          <input type="checkbox" checked={sv.logged} disabled={!canLog(sr.text)} onchange={(e) => toggleLogInSheet(e.currentTarget.checked)} />
          <span>{$trs('mobC.fw.logRule')}</span>
        </label>
        <div class="pc-list">
          <button class="pc-act" type="button" disabled={sr.index === 0} onclick={() => moveInSheet(-1)}><PixIcon name="chevron-up" size={14} /> {$trs('mobC.fw.up')}</button>
          <button class="pc-act" type="button" disabled={sr.index === sr.last} onclick={() => moveInSheet(1)}><PixIcon name="chevron-down" size={14} /> {$trs('mobC.fw.down')}</button>
          <button class="pc-act dng" type="button" onclick={deleteInSheet}>{$trs('fw.rule.delete')}</button>
        </div>
        <div class="row end">
          <button class="btn" type="button" onclick={() => (ruleSheet = null)}>{$trs('common.cancel')}</button>
          <button class="btn primary" type="submit" disabled={ruleBusy}>{$trs('mobC.fw.toDraft')}</button>
        </div>
      </form>
    </Modal>
  {/if}
{/if}

{#if addOpen && $isPhone}
  <Modal wide title={$trs('fw.add.title')} labelId="fw-add-title" onclose={() => (addOpen = false)}>
    <form
      class="stack addm"
      onsubmit={(event) => {
        event.preventDefault();
        void addAndClose();
      }}
    >
      <div class="field">
        <span class="label">{$trs('fw.add.chain')}</span>
        <Select bind:value={addChain} ariaLabel={$trs('fw.add.chain')} options={CHAIN_NAMES.map((c) => ({ value: c, label: c }))} />
      </div>
      <div class="field">
        <span class="label">{$trs('fw.add.action')}</span>
        <Select
          bind:value={addAction}
          ariaLabel={$trs('fw.add.action')}
          options={(['accept', 'drop', 'reject'] as RuleAction[]).map((a) => ({ value: a, label: a }))}
        />
      </div>
      <div class="field">
        <span class="label">{$trs('fw.add.proto')}</span>
        <Select
          bind:value={addProto}
          ariaLabel={$trs('fw.add.proto')}
          options={[
            ...(['tcp', 'udp', 'icmp'] as RuleProto[]).map((p) => ({ value: p, label: p as string })),
            { value: 'any' as RuleProto, label: $trs('fw.add.protoAny') },
          ]}
        />
      </div>
      <label class="field">
        <span class="label">{$trs('fw.add.port')}</span>
        <input class="input mono" type="text" inputmode="numeric" autocomplete="off" spellcheck="false" bind:value={addPort} oninput={() => (formError = null)} />
      </label>
      <label class="field">
        <span class="label">{$trs('fw.add.source')}</span>
        <SourceInput cls="input mono" lists={shownSets} ariaLabel={$trs('fw.add.source')} bind:value={addSource} oninput={() => (formError = null)} />
      </label>
      <label class="field">
        <span class="label">{$trs('fw.add.comment')}</span>
        <input class="input" type="text" autocomplete="off" bind:value={addComment} oninput={() => (formError = null)} />
      </label>
      {#if formError}<p class="error small" role="alert">{formError}</p>{/if}

      <button class="link" type="button" aria-expanded={rawOpen} onclick={() => (rawOpen = !rawOpen)}>{$trs('fw.add.rawToggle')}</button>
      {#if rawOpen}
        <div class="field">
          <input
            class="input mono"
            type="text"
            autocomplete="off"
            autocapitalize="off"
            spellcheck="false"
            placeholder={$trs('fw.add.rawPlaceholder')}
            aria-label={$trs('fw.add.rawToggle')}
            bind:value={rawText}
            oninput={() => (rawError = null)}
            onkeydown={(e) => {
              if (e.key === 'Enter') {
                e.preventDefault();
                void addRawAndClose();
              }
            }}
          />
          <button class="btn" type="button" disabled={addBusy || !rawText.trim()} onclick={() => void addRawAndClose()}>{$trs('mobC.fw.addRaw')}</button>
          {#if rawError}<p class="error small" role="alert">{rawError}</p>{/if}
        </div>
      {/if}

      <div class="row end">
        <button class="btn" type="button" onclick={() => (addOpen = false)}>{$trs('common.cancel')}</button>
        <button class="btn primary" type="submit" disabled={addBusy}>{addBusy ? $trs('fw.add.checking') : $trs('fw.add.submit')}</button>
      </div>
    </form>
  </Modal>
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
    gap: 18px;
  }

  h3 {
    margin: 0;
    font-size: 16px;
    line-height: 1.2;
  }

  .rules {
    display: flex;
    flex-direction: column;
    gap: 11px;
    min-width: 0;
  }

  .cap {
    margin: 0;
    font-size: 14px;
    font-weight: 400;
    color: var(--muted);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }

  .sect {
    font-family: var(--font-display);
    font-size: 15px;
    font-weight: 600;
  }

  .status {
    margin: 0;
    color: var(--muted);
    font-size: 11px;
  }

  .warn,
  .draft,
  .pending {
    margin: 0;
    padding: 11px;
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
    gap: 9px;
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
    font-size: 40px;
    line-height: 1;
    color: var(--warn);
  }

  .countdown.expired {
    font-size: 20px;
    color: var(--crit);
  }

  .digits {
    font-variant-numeric: tabular-nums;
  }

  .changes {
    margin: 0;
    padding-left: 16px;
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
    gap: 7px;
  }

  .tabsrow .count {
    margin-left: 4px;
    color: var(--muted);
  }

  .tabsrow :global(.btn),
  .tabsrow :global(.chip) {
    display: inline-flex;
    align-items: center;
    height: 32px;
    padding-block: 0;
  }

  .chain,
  .add-block,
  .ruleset-block {
    display: flex;
    flex-direction: column;
    gap: 5px;
    min-width: 0;
  }

  .chain-head,
  .ruleset-head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 7px;
    margin-bottom: 4px;
  }

  .chain-title {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: 9px;
  }

  .chain-name {
    font-family: var(--font-display);
    font-size: 15px;
    font-weight: 600;
  }

  .hook {
    color: var(--muted);
    font-size: 11px;
  }

  .tw {
    overflow-x: auto;
    min-width: 0;
  }

  .tbl {
    width: 100%;
    border-collapse: collapse;
    font-size: 11px;
  }

  .tbl th {
    font-family: var(--font-display);
    font-size: 11px;
    font-weight: 400;
    text-transform: uppercase;
    letter-spacing: 0.08em;
    color: var(--muted);
    text-align: left;
    padding: 5px 7px;
    border-bottom: 2px solid var(--line);
    white-space: nowrap;
  }

  .tbl td {
    padding: 6px 7px;
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

  .sw {
    position: relative;
    width: 34px;
    height: 20px;
    padding: 0;
    border: 2px solid var(--line);
    background: var(--seg-off);
    cursor: pointer;
  }

  .sw::after {
    content: '';
    position: absolute;
    top: 2px;
    left: 2px;
    width: 13px;
    height: 13px;
    background: var(--ink);
    transition: transform 0.15s;
  }

  .sw[aria-pressed='true'] {
    background: var(--good);
  }

  .sw[aria-pressed='true']::after {
    transform: translateX(16px);
    background: var(--surface);
  }

  .sw:disabled {
    opacity: 0.4;
    cursor: not-allowed;
  }

  .tabsrow .cnt {
    margin-left: 4px;
    line-height: 1.1;
  }

  .act {
    display: inline-block;
    padding: 1px 7px;
    border: 2px solid var(--muted);
    color: var(--muted);
    font-size: 11px;
    white-space: nowrap;
  }

  .act.accept {
    border-color: var(--good);
    color: var(--good);
  }

  .act.drop {
    border-color: var(--crit);
    color: var(--crit);
  }

  .act.reject {
    border-color: var(--warn);
    color: var(--warn);
  }

  .port,
  .src {
    overflow-wrap: anywhere;
  }

  .notebox {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .lock {
    margin: 4px 0 0;
  }

  .tabsrow .btn[aria-pressed='true'] {
    background: var(--ink);
    color: var(--surface);
  }

  .rule {
    font-size: 11px;
    overflow-wrap: anywhere;
    min-width: 0;
  }

  .empty {
    color: var(--muted);
    font-size: 11px;
  }

  .acts {
    width: 1%;
  }

  .ib {
    width: 23px;
    height: 23px;
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
    font-size: 13px;
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
    padding: 5px 7px;
    min-width: 0;
    font: inherit;
    font-size: 12px;
  }

  .fw :global(.ctl)::placeholder {
    color: var(--muted);
  }

  .fw :global(.ctl.w) {
    width: 153px;
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
    gap: 7px;
  }

  /* One height for every control of the row: the button is the tallest. */
  .inl :global(.ctl),
  .inl :global(.sel),
  .inl :global(.btn) {
    box-sizing: border-box;
    height: 32px;
    padding-block: 0;
  }

  .inl :global(.btn) {
    display: inline-flex;
    align-items: center;
  }

  .errt {
    margin: 0;
    min-height: 14px;
    color: var(--crit);
    font-size: 11px;
  }

  .link {
    align-self: flex-start;
    padding: 0;
    background: none;
    border: 0;
    color: var(--accent);
    font-family: var(--font-display);
    font-size: 12px;
    text-decoration: underline;
    text-underline-offset: 3px;
    cursor: pointer;
  }

  .ruleset {
    margin: 0;
    padding: 11px;
    max-height: 324px;
    overflow: auto;
    background: #14181e;
    color: #e6e9ef;
    border: 2px solid var(--line);
    box-shadow: 3px 3px 0 var(--shade);
    font-size: 11px;
    line-height: 1.5;
    white-space: pre;
  }

  .docks {
    display: contents;
  }

  /* Phone: rows instead of the table, the draft / countdown pinned above the bottom bar. */
  @media (max-width: 720px) {
    .fw {
      gap: 11px;
    }

    .rtext {
      font-size: 12px;
      overflow-wrap: anywhere;
    }

    .pc-empty {
      margin: 0;
    }

    .lock {
      margin: 0;
    }

    .ruleset {
      max-height: none;
    }

    .addm {
      min-height: 100%;
    }

    .addm > :global(.row.end) {
      margin-top: auto !important;
    }

    .addm :global(.src) {
      display: flex;
      width: 100%;
    }

    .addm :global(.input) {
      width: 100%;
    }

    .link {
      min-height: 44px;
      font-size: 13px;
    }

    .docks {
      order: 90;
      position: sticky;
      bottom: calc(var(--bar-h) + var(--safe-b));
      z-index: 6;
      display: flex;
      flex-direction: column;
      /* The negative bottom margin cancels the 16 px page padding: at the end of the scroll the bar still lies on the tab bar. */
      margin: 0 -11px -14px;
      /* Both blocks (confirm and draft) together must not eat the whole screen: the dock scrolls inside. */
      max-height: 55dvh;
      overflow-y: auto;
      overscroll-behavior: contain;
      background: var(--surface);
      border-top: 2px solid var(--warn);
      box-shadow: 0 -4px 0 var(--shade);
    }

    .docks:not(:has(*)) {
      display: none;
    }

    .pending,
    .draft {
      padding: 9px 11px;
      gap: 7px;
      border-width: 0;
      box-shadow: none;
    }

    .pending + .draft {
      border-top: 2px dashed var(--seg-off);
    }

    .pending h3,
    .draft h3 {
      font-size: 16px;
    }

    .pending p,
    .draft p {
      font-size: 11px;
    }

    .countdown {
      font-size: 25px;
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

    .dchanges summary {
      display: flex;
      align-items: center;
      min-height: 29px;
      font-size: 11px;
      cursor: pointer;
    }
  }
</style>
