<script lang="ts">
  import { untrack } from 'svelte';
  import type { GwDevice } from '../../../api';
  import { locale, tr } from '../../../i18n';
  import Modal from '../../Modal.svelte';
  import { formatDateTime } from '../format';
  import { nameSrcKey, portTag, whyText } from '../gatewayNet';

  let {
    device,
    netOwn,
    canWrite,
    onclose,
    onsave,
  }: {
    device: GwDevice;
    /** The network of the device is connected to the agent directly (so its MAC is real). */
    netOwn: boolean;
    canWrite: boolean;
    onclose: () => void;
    /** Saves the changed fields; resolves with an error text, or null when it worked. */
    onsave: (patch: { own_name?: string; excluded?: boolean }) => Promise<string | null>;
  } = $props();

  // The draft starts from the device as it is now; the list is refreshed in the background and must not reset it.
  const start = untrack(() => ({ name: device.own_name ?? '', excluded: device.excluded }));
  let ownName = $state(start.name);
  let excluded = $state(start.excluded);
  let busy = $state(false);
  let error = $state<string | null>(null);

  const patch = $derived.by(() => {
    const p: { own_name?: string; excluded?: boolean } = {};
    if (ownName.trim() !== start.name) p.own_name = ownName.trim();
    if (excluded !== start.excluded) p.excluded = excluded;
    return p;
  });
  const changed = $derived(Object.keys(patch).length > 0);

  const title = $derived(device.own_name || device.name || $tr('gw.dlg.noName'));
  const why = $derived((device.why ?? []).map((w) => whyText(w, $tr)).join(', '));
  const upnpText = $derived(device.upnp ? [device.upnp.model, device.upnp.maker].filter(Boolean).join(' · ') : '');
  const viaLabel = (v: string): string => (['arp', 'ping', 'ports'].includes(v) ? $tr(`gw.via.${v}`) : v);

  async function save(): Promise<void> {
    if (busy || !changed || !canWrite) return;
    busy = true;
    error = null;
    error = await onsave(patch);
    busy = false;
    if (!error) onclose();
  }
</script>

<Modal title={title} labelId="gw-dev" {onclose}>
  <form class="stack" onsubmit={(e) => { e.preventDefault(); void save(); }}>
    <div class="row">
      <span class="mono ip">{device.ip}</span>
      {#if device.online}<span class="gw-badge ok"><span class="gw-dot"></span>{$tr('gw.dev.online')}</span>{:else}<span class="gw-badge muted"><span class="gw-dot"></span>{$tr('gw.dev.offline')}</span>{/if}
      {#if device.known_server}<span class="gw-tag acc">{$tr('gw.badge.agent', { name: device.known_server })}</span>{/if}
      {#if device.type}<span class="gw-tag type">{$tr(`gw.type.${device.type}`)}</span>{/if}
      {#if device.is_new}<span class="gw-tag warn">{$tr('gw.badge.new')}</span>{/if}
      {#if device.excluded}<span class="gw-tag">{$tr('gw.badge.excluded')}</span>{/if}
      {#if device.hints?.includes('debian') && !device.known_server}<span class="gw-tag">{$tr('gw.badge.debian')}</span>{/if}
    </div>

    <section class="sec">
      <h3>{$tr('gw.dlg.known')}</h3>
      <dl class="kv">
        <dt>{$tr('gw.dlg.addr')}</dt><dd class="mono">{$tr('gw.dlg.addrNet', { ip: device.ip, net: device.net })}</dd>
        <dt>{$tr('gw.dlg.mac')}</dt>
        <dd class="mono">{device.mac ? device.mac : netOwn ? '—' : $tr('gw.dlg.behindRouter')}{#if device.random_mac}<span class="muted"> · {$tr('gw.dev.randomMac')}</span>{/if}</dd>
        {#if !device.random_mac}<dt>{$tr('gw.dlg.vendor')}</dt><dd>{device.vendor || '—'}</dd>{/if}
        {#if device.type}
          <dt>{$tr('gw.dev.type')}</dt>
          <dd><b>{$tr(`gw.type.${device.type}`)}</b>{#if why}<span class="muted">&nbsp;· {$tr('gw.dev.why', { why })}</span>{/if}</dd>
        {/if}
        <dt>{$tr('gw.dlg.name')}</dt>
        <dd>
          {#if device.name}{device.name}{#if nameSrcKey(device.name_src)}<span class="muted"> ({$tr(nameSrcKey(device.name_src) ?? '')})</span>{/if}{:else}{$tr('gw.dlg.nameUnknown')}{/if}
        </dd>
        <dt>{$tr('gw.dlg.found')}</dt><dd>{device.via.map(viaLabel).join(', ') || '—'}</dd>
        <dt>{$tr('gw.dlg.first')}</dt><dd>{formatDateTime(device.first_seen, $locale)}</dd>
        <dt>{$tr('gw.dlg.last')}</dt><dd>{device.online ? $tr('gw.dev.justNow') : formatDateTime(device.last_seen, $locale)}</dd>
      </dl>
    </section>

    <section class="sec">
      <h3>{$tr('gw.dlg.ports')}</h3>
      {#if device.ports.length === 0}
        <p class="muted small">{$tr('gw.dlg.noPorts')}</p>
      {:else}
        <dl class="kv">
          {#each device.ports as p (p.port)}
            <dt>{portTag(p.port)}</dt><dd>{#if p.banner}{p.banner}{:else}<span class="muted">—</span>{/if}</dd>
          {/each}
        </dl>
      {/if}
    </section>

    {#if device.svc?.length || device.upnp}
      <section class="sec">
        <h3>{$tr('gw.dev.services')}</h3>
        <dl class="kv">
          {#if device.svc?.length}
            <dt>mDNS</dt><dd>{#each device.svc as s (s)}<span class="gw-tag">{s}</span>{/each}</dd>
          {/if}
          {#if device.upnp}
            <dt>UPnP</dt>
            <dd>{upnpText}{#if device.upnp.type}{upnpText ? ' ' : ''}<span class="muted">({device.upnp.type})</span>{/if}</dd>
          {/if}
        </dl>
      </section>
    {/if}

    <section class="sec">
      <h3>{$tr('gw.dlg.ownName')}</h3>
      <input
        class="input"
        bind:value={ownName}
        maxlength="100"
        readonly={!canWrite}
        placeholder={$tr('gw.dlg.ownNamePh')}
        aria-label={$tr('gw.dlg.ownName')}
        autocomplete="off"
      />
      <p class="hint hintrow">{$tr('gw.dlg.ownNameHint')}</p>
    </section>

    <section class="sec">
      <h3>{$tr('gw.dlg.map')}</h3>
      <button class="gw-sw" class:on={excluded} type="button" role="switch" aria-checked={excluded} disabled={!canWrite} onclick={() => (excluded = !excluded)}>
        <i></i><span>{$tr('gw.dlg.exclude')}</span>
      </button>
      <p class="hint hintrow">{$tr('gw.dlg.excludeHint')}</p>
    </section>

    {#if error}<p class="error" role="alert">{error}</p>{/if}
    <div class="row end">
      <button class="btn" type="button" onclick={onclose}>{$tr('common.close')}</button>
      {#if canWrite}<button class="btn primary" type="submit" disabled={busy || !changed}>{$tr('gw.dlg.save')}</button>{/if}
    </div>
  </form>
</Modal>

<style>
  .ip {
    font-weight: 600;
  }

  .sec h3 {
    margin: 0 0 8px;
    font-family: var(--font-display);
    font-weight: 400;
    font-size: 13px;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--muted);
  }

  .kv {
    display: grid;
    grid-template-columns: 130px 1fr;
    gap: 4px 12px;
    margin: 0;
    font-size: 13px;
  }

  .kv dt {
    color: var(--muted);
  }

  .kv dd {
    margin: 0;
    overflow-wrap: anywhere;
  }

  .hintrow {
    margin-top: 6px;
  }

  @media (max-width: 720px) {
    .kv {
      grid-template-columns: 1fr;
      gap: 0;
    }

    .kv dt {
      margin-top: 8px;
      font-size: 12px;
    }
  }
</style>
