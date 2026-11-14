<script lang="ts">
  import type { Sample, ServerDetail } from '../../api';
  import { locale, tr } from '../../i18n';
  import Segments from '../ui/Segments.svelte';
  import MiniBars from './MiniBars.svelte';
  import { formatBytes, formatDateTime, formatLoad, formatPct, formatRate, formatUptime } from './format';
  import type { LiveSample } from './model';

  let {
    server,
    sample,
    live,
    online,
  }: {
    server: ServerDetail;
    sample: Sample | null;
    live: LiveSample[];
    online: boolean;
  } = $props();

  const disks = $derived(sample ? sample.disks.slice(0, 3) : []);
  const netMax = $derived(Math.max(1, ...live.map((s) => Math.max(s.rx ?? 0, s.tx ?? 0))));

  const info = $derived.by(() => {
    const T = $tr;
    const sys = server.system;
    return [
      { k: 'srv.sys.os', v: sys?.os ?? server.os },
      { k: 'srv.sys.kernel', v: sys?.kernel ?? '—' },
      { k: 'srv.sys.arch', v: sys?.arch ?? '—' },
      { k: 'srv.sys.cpu', v: sys ? T('srv.sys.cpuValue', { model: sys.cpu_model, cores: sys.cpu_cores }) : '—' },
      { k: 'srv.sys.memory', v: sys ? formatBytes(sys.mem_total) : '—' },
      { k: 'srv.sys.uptime', v: sample ? formatUptime(sample.uptime, T) : '—' },
      { k: 'srv.sys.boot', v: sample ? formatDateTime(sample.boot_time, $locale) : '—' },
      { k: 'srv.sys.agent', v: server.agent_version || '—' },
      { k: 'srv.sys.lastSeen', v: server.last_seen_at ? formatDateTime(server.last_seen_at, $locale) : '—' },
    ];
  });
</script>

<div class="overview" class:dim={!online}>
  {#if !online}
    <p class="notice" role="status">{$tr('srv.offlineNotice')}</p>
  {/if}

  <div class="grid-top">
    <section class="card panel">
      <h3>{$tr('srv.ov.cpu')}</h3>
      <p class="big">{formatPct(sample?.cpu)}</p>
      <MiniBars values={live.map((s) => s.cpu)} max={100} />
      <p class="small muted">{$tr('srv.ov.load', { values: sample ? formatLoad(sample.load) : '—' })}</p>
    </section>

    <section class="card panel">
      <h3>{$tr('srv.ov.ram')}</h3>
      <p class="big">{formatPct(sample?.mem_pct)} <span class="unit">· {formatBytes(sample?.mem_used)}</span></p>
      <MiniBars values={live.map((s) => s.mem)} max={100} />
      <p class="small muted">
        {$tr('srv.ov.memTotal', { total: formatBytes(sample?.mem_total), swap: formatBytes(sample?.swap_total) })}
      </p>
    </section>

    <section class="card panel">
      <h3>{$tr('srv.ov.net')}</h3>
      <p class="big">{formatRate(sample?.rx_bps)}</p>
      <MiniBars values={live.map((s) => s.rx)} max={netMax} />
      <p class="small muted">{$tr('srv.ov.netOut', { rate: formatRate(sample?.tx_bps) })}</p>
    </section>

    <section class="card panel">
      <h3>{$tr('srv.ov.disks')}</h3>
      {#each disks as d (d.mount)}
        <div class="disk">
          <div class="disk-head">
            <span class="mono">{d.mount}</span>
            <span>{formatPct(d.pct)}</span>
          </div>
          <Segments value={d.pct} />
          <p class="small muted">{formatBytes(d.used)} / {formatBytes(d.total)}</p>
        </div>
      {:else}
        <p class="small muted">{$tr('srv.ov.noDisks')}</p>
      {/each}
    </section>
  </div>

  <div class="grid-bottom">
    <section class="card panel">
      <h3>{$tr('srv.ov.interfaces')}</h3>
      {#if sample && sample.net.length > 0}
        <div class="table-wrap">
          <div class="grid-table" role="table">
            <div class="nrow head" role="row">
              <span role="columnheader">{$tr('srv.ov.ifName')}</span>
              <span role="columnheader">{$tr('srv.ov.ifState')}</span>
              <span role="columnheader">{$tr('srv.ov.ifIp')}</span>
              <span role="columnheader">{$tr('srv.ov.ifMac')}</span>
              <span role="columnheader" class="num">{$tr('srv.ov.ifRx')}</span>
              <span role="columnheader" class="num">{$tr('srv.ov.ifTx')}</span>
              <span role="columnheader" class="num">{$tr('srv.ov.ifRxTotal')}</span>
              <span role="columnheader" class="num">{$tr('srv.ov.ifTxTotal')}</span>
            </div>
            {#each sample.net as nic (nic.iface)}
              <div class="nrow" role="row">
                <span role="cell" class="mono">{nic.iface}</span>
                <span role="cell" class:up={nic.up}>{nic.up ? 'UP' : 'DOWN'}</span>
                <span role="cell" class="mono">{nic.ip || '—'}</span>
                <span role="cell" class="mono">{nic.mac || '—'}</span>
                <span role="cell" class="num">{formatRate(nic.rx_bps)}</span>
                <span role="cell" class="num">{formatRate(nic.tx_bps)}</span>
                <span role="cell" class="num">{formatBytes(nic.rx_total)}</span>
                <span role="cell" class="num">{formatBytes(nic.tx_total)}</span>
              </div>
            {/each}
          </div>
        </div>
      {:else}
        <p class="small muted">{$tr('srv.ov.noIfaces')}</p>
      {/if}
    </section>

    <section class="card panel">
      <h3>{$tr('srv.ov.system')}</h3>
      <dl class="info">
        {#each info as row (row.k)}
          <div>
            <dt>{$tr(row.k)}</dt>
            <dd class="mono">{row.v}</dd>
          </div>
        {/each}
      </dl>
    </section>
  </div>
</div>

<style>
  .overview {
    display: flex;
    flex-direction: column;
    gap: 16px;
  }

  .overview.dim {
    opacity: 0.6;
  }

  .notice {
    margin: 0;
    padding: 8px 12px;
    border: 2px solid var(--warn);
    color: var(--warn);
  }

  .grid-top {
    display: grid;
    gap: 16px;
    grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
  }

  .grid-bottom {
    display: grid;
    gap: 16px;
    grid-template-columns: 1fr;
    align-items: start;
  }

  @media (min-width: 760px) {
    .grid-bottom {
      grid-template-columns: minmax(0, 1.4fr) minmax(0, 1fr);
    }
  }

  .panel {
    display: flex;
    flex-direction: column;
    gap: 12px;
    min-width: 0;
  }

  .panel h3 {
    margin: 0;
    font-size: 16px;
    color: var(--muted);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }

  .big {
    margin: 0;
    font-variation-settings: "ELSH" 40;
    font-family: var(--font-display);
    font-size: 30px;
    line-height: 1.1;
  }

  .unit {
    font-size: 16px;
    color: var(--muted);
  }

  .disk {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .disk-head {
    display: flex;
    justify-content: space-between;
    gap: 8px;
  }

  .table-wrap {
    overflow-x: auto;
  }

  .grid-table {
    display: flex;
    flex-direction: column;
    min-width: 640px;
  }

  .nrow {
    display: grid;
    grid-template-columns: 0.7fr 0.8fr 1fr 1.3fr 1fr 1fr 1fr 1fr;
    gap: 8px;
    align-items: center;
    padding: 6px 8px;
    border-bottom: 1px dashed var(--line);
    white-space: nowrap;
  }

  .nrow.head {
    color: var(--muted);
    border-bottom: 2px solid var(--line);
  }

  .num {
    text-align: right;
  }

  .up {
    color: var(--good);
  }

  .info {
    margin: 0;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .info > div {
    display: flex;
    justify-content: space-between;
    gap: 12px;
  }

  .info dt {
    color: var(--muted);
  }

  .info dd {
    margin: 0;
    text-align: right;
    overflow-wrap: anywhere;
  }
</style>
