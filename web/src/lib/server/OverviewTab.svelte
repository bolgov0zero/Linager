<script lang="ts">
  import type { Sample, ServerDetail } from '../../api';
  import { locale, tr } from '../../i18n';
  import { isPhone } from '../ui/viewport';
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
      {#if $isPhone}
        <Segments value={sample?.cpu ?? null} />
      {:else}
        <MiniBars values={live.map((s) => s.cpu)} max={100} />
      {/if}
      <p class="small muted">{$tr('srv.ov.load', { values: sample ? formatLoad(sample.load) : '—' })}</p>
    </section>

    <section class="card panel">
      <h3>{$tr('srv.ov.ram')}</h3>
      <p class="big">{formatPct(sample?.mem_pct)} <span class="unit">· {formatBytes(sample?.mem_used)}</span></p>
      {#if $isPhone}
        <Segments value={sample?.mem_pct ?? null} />
      {:else}
        <MiniBars values={live.map((s) => s.mem)} max={100} />
      {/if}
      <p class="small muted">
        {$tr('srv.ov.memTotal', { total: formatBytes(sample?.mem_total), swap: formatBytes(sample?.swap_total) })}
      </p>
    </section>

    <section class="card panel">
      <h3>{$isPhone ? $tr('m.ov.rx') : $tr('srv.ov.net')}</h3>
      <p class="big">{formatRate(sample?.rx_bps)}</p>
      <MiniBars values={live.map((s) => s.rx)} max={netMax} />
      {#if !$isPhone}
        <p class="small muted">{$tr('srv.ov.netOut', { rate: formatRate(sample?.tx_bps) })}</p>
      {/if}
    </section>

    {#if $isPhone}
      <section class="card panel">
        <h3>{$tr('m.ov.tx')}</h3>
        <p class="big">{formatRate(sample?.tx_bps)}</p>
        <MiniBars values={live.map((s) => s.tx)} max={netMax} color="var(--series-2, var(--accent))" />
      </section>
    {/if}

    <section class="card panel disks">
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
      {#if sample && sample.net.length > 0 && $isPhone}
        <ul class="nic-list">
          {#each sample.net as nic (nic.iface)}
            <li>
              <div class="nic-main">
                <b class="mono">{nic.iface}</b>
                <span class="small muted mono">{nic.ip || '—'}{nic.up ? '' : ' · DOWN'}</span>
              </div>
              <div class="nic-rate small">
                <span>↓ {formatRate(nic.rx_bps)}</span>
                <span>↑ {formatRate(nic.tx_bps)}</span>
              </div>
            </li>
          {/each}
        </ul>
      {:else if sample && sample.net.length > 0}
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
    gap: 14px;
  }

  .overview.dim {
    opacity: 0.6;
  }

  .notice {
    margin: 0;
    padding: 7px 11px;
    border: 2px solid var(--warn);
    color: var(--warn);
  }

  .grid-top {
    display: grid;
    gap: 14px;
    grid-template-columns: repeat(auto-fit, minmax(198px, 1fr));
  }

  .grid-bottom {
    display: grid;
    gap: 14px;
    grid-template-columns: 1fr;
    align-items: start;
  }

  .panel {
    display: flex;
    flex-direction: column;
    gap: 11px;
    min-width: 0;
  }

  .panel h3 {
    margin: 0;
    font-size: 14px;
    color: var(--muted);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }

  .big {
    margin: 0;
    font-variation-settings: "ELSH" 40;
    font-family: var(--font-display);
    font-size: 27px;
    line-height: 1.1;
  }

  .unit {
    font-size: 14px;
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
    gap: 7px;
  }

  .table-wrap {
    overflow-x: auto;
  }

  .grid-table {
    display: flex;
    flex-direction: column;
    min-width: 837px;
  }

  .nrow {
    display: grid;
    grid-template-columns: minmax(135px, 1.4fr) 90px minmax(99px, 1fr) minmax(135px, 1.3fr) repeat(4, minmax(76px, 1fr));
    gap: 7px;
    align-items: center;
    padding: 5px 7px;
    border-bottom: 1px dashed var(--line);
    white-space: nowrap;
  }

  .nrow.head {
    color: var(--muted);
    border-bottom: 2px solid var(--line);
  }

  .nrow > :global(span) {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .num {
    text-align: right;
  }

  .up {
    color: var(--good);
  }

  /* The interfaces table needs the full width, so the system block sits below it with its rows in several columns. */
  .info {
    margin: 0;
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(270px, 1fr));
    gap: 5px 36px;
  }

  .info > div {
    display: flex;
    justify-content: space-between;
    gap: 11px;
  }

  .info dt {
    color: var(--muted);
  }

  .info dd {
    margin: 0;
    text-align: right;
    overflow-wrap: anywhere;
  }

  /* Phones: the metric tiles sit two by two, disks and interfaces take the full width, "System" is a plain key/value list. */
  .nic-list {
    margin: 0;
    padding: 0;
    list-style: none;
  }

  @media (max-width: 720px) {
    .overview {
      gap: 11px;
    }

    .grid-top,
    .grid-bottom {
      gap: 11px;
    }

    .grid-top {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }

    .panel.card {
      gap: 7px;
    }

    .panel.disks {
      grid-column: 1 / -1;
      gap: 7px;
    }

    .disk {
      gap: 2px;
    }

    .panel h3 {
      font-size: 13px;
    }

    .big {
      font-size: 23px;
    }

    .unit {
      font-size: 11px;
    }

    .panel :global(.mini) {
      height: 29px;
    }

    .nic-list li {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 11px;
      min-height: 52px;
      padding: 5px 0;
      border-top: 2px dashed var(--seg-off);
    }

    .nic-list li:first-child {
      border-top: 0;
    }

    .nic-main,
    .nic-rate {
      display: flex;
      flex-direction: column;
      min-width: 0;
    }

    .nic-rate {
      align-items: flex-end;
      white-space: nowrap;
    }

    .info {
      grid-template-columns: 1fr;
      gap: 0;
    }

    .info > div {
      min-height: 40px;
      align-items: center;
      border-top: 2px dashed var(--seg-off);
    }

    .info > div:first-child {
      border-top: 0;
    }
  }
</style>
