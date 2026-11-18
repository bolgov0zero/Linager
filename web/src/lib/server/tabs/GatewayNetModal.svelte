<script lang="ts">
  import { tick } from 'svelte';
  import type { GwNet, GwNetEdit, GwOwnNet } from '../../../api';
  import { locale, tr } from '../../../i18n';
  import { pluralForm } from '../../dashboard/format';
  import Modal from '../../Modal.svelte';
  import { isPhone } from '../../ui/viewport';
  import { clash, MAX_NETS, parseNet, split24 } from '../gatewayNet';

  let {
    nets,
    ownNets,
    onclose,
    onadd,
  }: {
    /** The networks the gateway has now. */
    nets: GwNet[];
    ownNets: GwOwnNet[];
    onclose: () => void;
    /** Adds the networks (the whole configuration is saved); resolves with an error text, or null when it worked. */
    onadd: (items: GwNetEdit[]) => Promise<string | null>;
  } = $props();

  const TEMPLATES = ['192.168.0.0/24', '10.0.0.0/24', '172.16.0.0/24'];

  let text = $state('');
  let ports = $state(true);
  let names = $state(true);
  let confirmPublic = $state(false);
  let busy = $state(false);
  let error = $state<string | null>(null);
  let input = $state<HTMLInputElement | undefined>(undefined);

  const free = $derived(MAX_NETS - nets.length);
  const existing = $derived(nets.map((n) => n.cidr));
  const parsed = $derived(parseNet(text, free));

  /** What the typed text comes to: a message, and the networks that would be added. */
  const verdict = $derived.by<{ refuse: string | null; add: string[]; code: string | null }>(() => {
    const r = parsed;
    if (r.ok) {
      if (free <= 0) return { refuse: $tr('gw.net.max', { max: MAX_NETS }), add: [], code: 'max' };
      const c = clash(r.cidr, existing);
      if (c) return { refuse: c.same ? $tr('gw.net.dup') : $tr('gw.net.overlap', { cidr: c.cidr }), add: [], code: 'clash' };
      return { refuse: null, add: [r.cidr], code: null };
    }
    if (r.error === 'empty') return { refuse: null, add: [], code: 'empty' };
    if (r.error === 'too_big') {
      const bits = r.bits ?? 0;
      if (r.split && r.cidr) {
        const parts = split24(r.cidr);
        const c = parts.map((p) => clash(p, existing)).find((x) => x !== null);
        if (c) return { refuse: $tr('gw.net.overlapAny', { cidr: c.cidr }), add: [], code: 'clash' };
        return { refuse: $tr('gw.net.err.tooBigSplit', { bits, parts: parts24(parts.length) }), add: parts, code: 'split' };
      }
      const all = bits >= 16 ? 2 ** (24 - bits) : 0;
      return {
        refuse: all > 0 ? $tr('gw.net.err.tooBigFit', { bits, parts: parts24(all), free: Math.max(0, free) }) : $tr('gw.net.err.tooBigHuge', { bits }),
        add: [],
        code: 'big',
      };
    }
    const key: Record<string, string> = {
      syntax: 'gw.net.err.syntax',
      ipv6: 'gw.net.err.ipv6',
      loopback: 'gw.net.err.loopback',
      reserved: 'gw.net.err.reserved',
      too_small: 'gw.net.err.tooSmall',
    };
    return { refuse: $tr(key[r.error] ?? 'gw.net.err.syntax'), add: [], code: r.error };
  });

  const kind = $derived(parsed.kind);
  const needConfirm = $derived(verdict.add.length > 0 && kind === 'public');
  const own = $derived(parsed.ok ? ownNets.find((o) => o.cidr === parsed.cidr) : undefined);
  const canAdd = $derived(parsed.ok && verdict.add.length > 0 && !busy && (!needConfirm || confirmPublic));

  /** "4 networks /24" in the form of the language. */
  function parts24(n: number): string {
    return $tr(`gw.net.n24.${pluralForm($locale, n)}`, { n });
  }

  $effect(() => {
    const el = input;
    if (el && !$isPhone) void tick().then(() => el.focus());
  });

  async function add(): Promise<void> {
    if (busy || verdict.add.length === 0 || (needConfirm && !confirmPublic)) return;
    busy = true;
    error = null;
    const items: GwNetEdit[] = verdict.add.map((cidr) => ({ cidr, on: true, ports, names, ...(kind === 'public' ? { confirm_public: true } : {}) }));
    error = await onadd(items);
    busy = false;
    if (!error) onclose();
  }
</script>

<Modal title={$tr('gw.net.title')} labelId="gw-net" {onclose}>
  <form class="stack" onsubmit={(e) => { e.preventDefault(); if (parsed.ok) void add(); }}>
    <label class="field">
      <span class="label">{$tr('gw.net.label')}</span>
      <input
        class="input mono"
        bind:this={input}
        bind:value={text}
        placeholder="192.168.30.0/24"
        autocomplete="off"
        autocapitalize="none"
        spellcheck="false"
        inputmode="decimal"
      />
    </label>
    <div class="row">
      <span class="hint">{$tr('gw.net.tpl')}</span>
      {#each TEMPLATES as t (t)}
        <button class="chip" type="button" onclick={() => (text = t)}>{t}</button>
      {/each}
    </div>

    <div class="stack info" aria-live="polite">
      {#if verdict.code === 'empty'}
        <p class="hint">{$tr('gw.net.hint')}</p>
      {:else if verdict.refuse}
        <div class="gw-msg err">{verdict.refuse}</div>
      {/if}
      {#if parsed.ok && verdict.add.length > 0}
        {#if !parsed.aligned}<div class="gw-msg">{$tr('gw.net.willAdd', { cidr: parsed.cidr })}</div>{/if}
        {#if own}
          <div class="gw-msg ok">{$tr('gw.net.okOwn', { iface: own.iface })}</div>
        {:else}
          <div class="gw-msg ok">{$tr('gw.net.okRouted')}</div>
        {/if}
        <p class="hint">{$tr('gw.net.hosts', { n: parsed.hosts })}</p>
      {/if}
      {#if needConfirm}
        <div class="gw-crit"><b>{$tr('gw.net.publicTitle')}</b> {$tr('gw.net.publicText')}</div>
        <label class="check"><input type="checkbox" bind:checked={confirmPublic} /><span>{$tr('gw.net.confirm')}</span></label>
      {/if}
      {#if verdict.code === 'split'}
        <div><button class="btn sm" type="button" disabled={busy || (needConfirm && !confirmPublic)} onclick={() => void add()}>{$tr('gw.net.addSplit', { parts: parts24(verdict.add.length) })}</button></div>
      {/if}
    </div>

    <div class="field">
      <span class="label">{$tr('gw.net.what')}</span>
      <div class="opts">
        <label class="opt on"><input type="checkbox" checked disabled /><span>{$tr('gw.net.optFind')}</span></label>
        <label class="opt" class:on={ports}><input type="checkbox" bind:checked={ports} /><span>{$tr('gw.net.optPorts')}<small class="muted">{$tr('gw.net.optPortsHint')}</small></span></label>
        <label class="opt" class:on={names}><input type="checkbox" bind:checked={names} /><span>{$tr('gw.net.optNames')}<small class="muted">{$tr('gw.net.optNamesHint')}</small></span></label>
      </div>
    </div>

    <p class="hint">{$tr('gw.net.count', { n: nets.length, max: MAX_NETS })}</p>
    {#if error}<p class="error" role="alert">{error}</p>{/if}
    <div class="row end">
      <button class="btn" type="button" onclick={onclose}>{$tr('common.cancel')}</button>
      <button class="btn primary" type="submit" disabled={!canAdd || verdict.code === 'split'}>{$tr('gw.net.do')}</button>
    </div>
  </form>
</Modal>

<style>
  .info {
    gap: 8px;
  }

  .opts {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .opt {
    display: flex;
    gap: 10px;
    align-items: flex-start;
    padding: 8px 10px;
    border: 2px solid var(--seg-off);
    cursor: pointer;
  }

  .opt.on {
    border-color: var(--line);
  }

  .opt small {
    display: block;
    font-size: 12px;
  }

  @media (max-width: 720px) {
    .opt {
      min-height: 48px;
    }
  }
</style>
