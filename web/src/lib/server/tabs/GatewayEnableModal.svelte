<script lang="ts">
  import { untrack } from 'svelte';
  import type { GwNetEdit, GwOwnNet } from '../../../api';
  import { tr } from '../../../i18n';
  import Modal from '../../Modal.svelte';
  import { parseNet } from '../gatewayNet';

  let {
    name,
    ownNets,
    onclose,
    onenable,
  }: {
    /** The name of the server whose agent will scan. */
    name: string;
    /** The networks the agent is connected to directly, as the agent reported them. */
    ownNets: GwOwnNet[];
    onclose: () => void;
    /** Saves the configuration with these networks; resolves with an error text, or null when it worked. */
    onenable: (nets: GwNetEdit[]) => Promise<string | null>;
  } = $props();

  // all own networks are ticked at the start
  let picked = $state<string[]>(untrack(() => ownNets.map((o) => o.cidr)));
  let confirmPublic = $state(false);
  let busy = $state(false);
  let error = $state<string | null>(null);

  // an own network with a public address (a server with a public interface) needs the same confirmation as a manual one
  const hasPublic = $derived(picked.some((c) => parseNet(c).kind === 'public'));
  const canGo = $derived(!busy && picked.length > 0 && (!hasPublic || confirmPublic));

  function toggle(cidr: string): void {
    picked = picked.includes(cidr) ? picked.filter((c) => c !== cidr) : [...picked, cidr];
  }

  async function go(): Promise<void> {
    if (!canGo) return;
    busy = true;
    error = null;
    const nets: GwNetEdit[] = ownNets
      .filter((o) => picked.includes(o.cidr))
      .map((o) => ({ cidr: o.cidr, on: true, ports: true, names: true, ...(hasPublic ? { confirm_public: true } : {}) }));
    error = await onenable(nets);
    busy = false;
    if (!error) onclose();
  }
</script>

<Modal title={$tr('gw.en.title')} labelId="gw-enable" {onclose}>
  <form class="stack" onsubmit={(e) => { e.preventDefault(); void go(); }}>
    <p>{$tr('gw.en.text', { name })}</p>
    <div class="nets">
      {#each ownNets as o (o.cidr)}
        <label class="net" class:on={picked.includes(o.cidr)}>
          <input type="checkbox" checked={picked.includes(o.cidr)} onchange={() => toggle(o.cidr)} />
          <span><b class="mono">{o.cidr}</b> · {o.iface}{o.vlan ? ` · ${o.vlan}` : ''}</span>
        </label>
      {/each}
    </div>
    {#if picked.length === 0}<p class="hint">{$tr('gw.en.pickOne')}</p>{/if}
    {#if hasPublic}
      <div class="gw-crit"><b>{$tr('gw.net.publicTitle')}</b> {$tr('gw.net.publicText')}</div>
      <label class="check"><input type="checkbox" bind:checked={confirmPublic} /><span>{$tr('gw.net.confirm')}</span></label>
    {/if}
    <div class="gw-warn">{$tr('gw.en.warn')}</div>
    <p class="hint">{$tr('gw.en.hint')}</p>
    {#if error}<p class="error" role="alert">{error}</p>{/if}
    <div class="row end">
      <button class="btn" type="button" onclick={onclose}>{$tr('common.cancel')}</button>
      <button class="btn primary" type="submit" disabled={!canGo}>{$tr('gw.en.do')}</button>
    </div>
  </form>
</Modal>

<style>
  .nets {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .net {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 6px 10px;
    border: 2px solid var(--seg-off);
    cursor: pointer;
  }

  .net.on {
    border-color: var(--line);
  }

  @media (max-width: 720px) {
    .net {
      min-height: 48px;
    }
  }
</style>
