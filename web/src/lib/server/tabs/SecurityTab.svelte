<script lang="ts">
  import { untrack } from 'svelte';
  import { api, type SecState, type ServerDetail } from '../../../api';
  import { locale, tr as T } from '../../../i18n';
  import { formatDateTime } from '../format';
  import { barHeights, keyLabel, postureChecks, worstTone } from './sec-util';
  import { unavailableKind } from './svc';

  let { server }: { server: ServerDetail } = $props();

  type LoadState = 'loading' | 'ready' | 'offline' | 'disabled' | 'error';

  const POLL_MS = 60_000; // the agent answers from a one minute cache

  let sec = $state<SecState | null>(null);
  let loadState = $state<LoadState>('loading');
  let busy = $state(false);
  let seq = 0;

  async function load(id: string): Promise<void> {
    const req = ++seq;
    busy = true;
    try {
      const data = await api.security(id);
      if (req !== seq) return;
      sec = data;
      loadState = 'ready';
    } catch (e) {
      if (req !== seq) return;
      loadState = unavailableKind(e) ?? 'error';
    } finally {
      if (req === seq) busy = false;
    }
  }

  $effect(() => {
    const id = server.id;
    untrack(() => {
      sec = null;
      loadState = 'loading';
      void load(id);
    });
    const timer = setInterval(() => void load(id), POLL_MS);
    return () => clearInterval(timer);
  });

  const checks = $derived(sec ? postureChecks(sec, server.sample) : []);
  const tone = $derived(worstTone(checks));
  const heights = $derived(sec ? barHeights(sec.failures.by_day) : []);
  const unreachable = $derived(!server.online || loadState === 'offline' || loadState === 'disabled');

  /** Weekday label of the 24 hours bucket i (the last one ends now). */
  function dayLabel(i: number, count: number): string {
    if (!sec) return '';
    const end = new Date((sec.generated_at - (count - 1 - i) * 86400) * 1000);
    return end.toLocaleDateString($locale, { weekday: 'short' });
  }

  const method = (m: string) => {
    const key = `sec.method.${m}`;
    const text = $T(key);
    return text === key ? m : text;
  };
</script>

<div class="sec">
  {#if unreachable}
    <p class="notice" role="status">{$T(loadState === 'disabled' ? 'svc.disabled' : 'svc.offline')}</p>
  {/if}

  <div class="head">
    <div>
      <p class="muted small">
        {$T('sec.period', { days: sec?.period_days ?? 7 })}
        {#if sec}· {$T('sec.updated', { time: formatDateTime(sec.generated_at, $locale) })}{/if}
      </p>
    </div>
    <button class="btn" type="button" disabled={busy || unreachable} onclick={() => void load(server.id)}>
      {$T('sec.refresh')}
    </button>
  </div>

  {#if loadState === 'loading'}
    <p class="muted">{$T('common.loading')}</p>
  {:else if loadState === 'error'}
    <p class="error" role="alert">{$T('sec.loadFailed')}</p>
  {/if}

  {#if sec}
    {#if !sec.ssh.installed}
      <p class="notice" role="status">{$T('sec.noSsh')}</p>
    {/if}
    {#if sec.journal.source === 'none'}
      <p class="notice" role="status">{$T('sec.noLog')}</p>
    {:else}
      {#if !sec.journal.covers_period}
        <p class="notice" role="status">
          {sec.journal.oldest > 0
            ? $T('sec.shortLog', { date: formatDateTime(sec.journal.oldest, $locale) })
            : $T('sec.shortLogUnknown')}
        </p>
      {/if}
      {#if sec.journal.truncated}
        <p class="notice" role="status">{$T('sec.truncated')}</p>
      {/if}
    {/if}

    <section class="card stack" aria-labelledby="sec-state">
      <h3 id="sec-state">
        {$T('sec.state')}
        <span class="badge tone-{tone}">{$T(`sec.verdict.${tone}`)}</span>
      </h3>
      <ul class="checks">
        {#each checks as c (c.id)}
          <li>
            <span class="clabel">{$T(c.label)}</span>
            <span class="cval tone-{c.tone}"><i class="dot" aria-hidden="true"></i>{$T(c.value, c.vars)}</span>
          </li>
        {/each}
      </ul>
    </section>

    <section class="card stack" aria-labelledby="sec-fail">
      <h3 id="sec-fail">{$T('sec.failures')}</h3>
      <div class="fail-top">
        <div class="big-wrap">
          <p class="big">{sec.failures.total.toLocaleString($locale)}</p>
          <p class="muted small">
            {$T('sec.failures.sub', { addresses: sec.failures.addresses, invalid: sec.failures.invalid_user_attempts })}
          </p>
        </div>
        <div class="bars" role="img" aria-label={$T('sec.failures.byDay')}>
          {#each sec.failures.by_day as n, i (i)}
            <div class="bar-col" title="{dayLabel(i, sec.failures.by_day.length)}: {n}">
              <span class="bar-num small muted">{n}</span>
              <span class="bar"><i style:height="{heights[i]}%"></i></span>
              <span class="bar-day small muted">{dayLabel(i, sec.failures.by_day.length)}</span>
            </div>
          {/each}
        </div>
      </div>

      {#if sec.failures.total === 0}
        <p class="muted">{$T('sec.failures.none')}</p>
      {:else}
        <div class="two">
          <div class="table-wrap">
            <table class="tbl">
              <thead>
                <tr>
                  <th scope="col">{$T('sec.col.ip')}</th>
                  <th scope="col" class="num">{$T('sec.col.attempts')}</th>
                  <th scope="col" class="num">{$T('sec.col.usersTried')}</th>
                  <th scope="col">{$T('sec.col.last')}</th>
                </tr>
              </thead>
              <tbody>
                {#each sec.failures.top_ips as r (r.ip)}
                  <tr>
                    <td class="mono">{r.ip}</td>
                    <td class="num">{r.count.toLocaleString($locale)}</td>
                    <td class="num">{r.users}</td>
                    <td class="small muted">{formatDateTime(r.last, $locale)}</td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
          <div class="table-wrap">
            <table class="tbl">
              <thead>
                <tr>
                  <th scope="col">{$T('sec.col.user')}</th>
                  <th scope="col" class="num">{$T('sec.col.attempts')}</th>
                  <th scope="col">{$T('sec.col.notes')}</th>
                </tr>
              </thead>
              <tbody>
                {#each sec.failures.top_users as r (r.user)}
                  <tr>
                    <td class="mono">{r.user || '—'}</td>
                    <td class="num">{r.count.toLocaleString($locale)}</td>
                    <td>{#if r.invalid}<span class="badge tone-muted">{$T('sec.badge.noUser')}</span>{/if}</td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
        </div>
      {/if}
    </section>

    <section class="card stack" aria-labelledby="sec-logins">
      <h3 id="sec-logins">{$T('sec.logins')}</h3>
      {#if sec.logins.length === 0}
        <p class="muted">{$T('sec.logins.none')}</p>
      {:else}
        <div class="table-wrap scroll">
          <table class="tbl">
            <thead>
              <tr>
                <th scope="col">{$T('sec.col.time')}</th>
                <th scope="col">{$T('sec.col.user')}</th>
                <th scope="col">{$T('sec.col.from')}</th>
                <th scope="col">{$T('sec.col.method')}</th>
                <th scope="col">{$T('sec.col.notes')}</th>
              </tr>
            </thead>
            <tbody>
              {#each sec.logins as l, i (i)}
                <tr>
                  <td class="small">{formatDateTime(l.time, $locale)}</td>
                  <td class="mono">{l.user}</td>
                  <td class="mono">{l.ip}</td>
                  <td>{method(l.method)}</td>
                  <td>
                    {#if l.prior_failures > 0}
                      <span class="badge tone-warn" title={$T('sec.badge.priorHint')}>{$T('sec.badge.prior', { n: l.prior_failures })}</span>
                    {/if}
                  </td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {/if}
    </section>

    <div class="two">
      <section class="card stack" aria-labelledby="sec-sessions">
        <h3 id="sec-sessions">{$T('sec.sessions')}</h3>
        {#if sec.sessions.length === 0}
          <p class="muted">{$T('sec.sessions.none')}</p>
        {:else}
          <ul class="plain">
            {#each sec.sessions as s, i (i)}
              <li>
                <span class="mono">{s.user}</span> · <span class="mono">{s.ip || '—'}</span>
                {#if s.since}<span class="muted small"> · {$T('sec.since', { time: formatDateTime(s.since, $locale) })}</span>{/if}
              </li>
            {/each}
          </ul>
        {/if}
      </section>

      <section class="card stack" aria-labelledby="sec-f2b">
        <h3 id="sec-f2b">fail2ban</h3>
        {#if !sec.fail2ban.installed}
          <p class="muted">{$T('sec.f2b.none')}</p>
        {:else if !sec.fail2ban.active}
          <p class="muted">{$T('sec.f2b.stopped')}</p>
        {:else if sec.fail2ban.jails.length === 0}
          <p class="muted">{$T('sec.f2b.nojails')}</p>
        {:else}
          {#each sec.fail2ban.jails as j (j.name)}
            <div class="jail">
              <p>
                <strong class="mono">{j.name}</strong>
                <span class="muted small">
                  · {$T('sec.f2b.banned', { n: j.currently_banned })} · {$T('sec.f2b.total', { n: j.total_banned })}
                </span>
              </p>
              {#if j.banned_ips.length > 0}
                <p class="ips">{#each j.banned_ips as ip (ip)}<span class="chip-ip mono">{ip}</span>{/each}</p>
              {/if}
            </div>
          {/each}
        {/if}
      </section>
    </div>

    <section class="card stack" aria-labelledby="sec-keys">
      <h3 id="sec-keys">{$T('sec.keys')}</h3>
      <p class="muted small">{$T('sec.keys.hint')}</p>
      {#if sec.keys.length === 0}
        <p class="muted">{$T('sec.keys.none')}</p>
      {:else}
        <div class="table-wrap">
          <table class="tbl">
            <thead>
              <tr>
                <th scope="col">{$T('sec.col.user')}</th>
                <th scope="col">{$T('sec.col.keyType')}</th>
                <th scope="col">{$T('sec.col.fingerprint')}</th>
                <th scope="col">{$T('sec.col.comment')}</th>
                <th scope="col">{$T('sec.col.fileChanged')}</th>
              </tr>
            </thead>
            <tbody>
              {#each sec.keys as k, i (i)}
                <tr>
                  <td class="mono">{k.user}</td>
                  <td>
                    {keyLabel(k.type, k.bits)}
                    {#if k.weak}<span class="badge tone-crit">{$T('sec.badge.weak')}</span>{/if}
                    {#if k.restricted}<span class="badge tone-muted">{$T('sec.badge.restricted')}</span>{/if}
                  </td>
                  <td class="mono small fp" title={k.file}>{k.fingerprint}</td>
                  <td class="small">{k.comment || '—'}</td>
                  <td class="small muted">{k.mtime ? formatDateTime(k.mtime, $locale) : '—'}</td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {/if}
    </section>
  {/if}
</div>

<style>
  .sec {
    display: flex;
    flex-direction: column;
    gap: 14px;
    min-width: 0;
  }

  .head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
  }

  .head p,
  h3 {
    margin: 0;
  }

  h3 {
    display: flex;
    align-items: center;
    gap: 10px;
    font-size: 16px;
    color: var(--muted);
    text-transform: uppercase;
    letter-spacing: 0.06em;
  }

  .notice {
    margin: 0;
    padding: 8px 12px;
    border: 2px solid var(--warn);
    color: var(--warn);
  }

  .badge {
    display: inline-block;
    padding: 1px 6px;
    border: 2px solid currentColor;
    font-size: 12px;
    font-family: var(--font-body);
    letter-spacing: 0;
    text-transform: none;
    white-space: nowrap;
  }

  .tone-ok {
    color: var(--good);
  }

  .tone-warn {
    color: var(--warn);
  }

  .tone-crit {
    color: var(--crit);
  }

  .tone-muted {
    color: var(--muted);
  }

  .checks {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
    gap: 8px 24px;
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .checks li {
    display: flex;
    justify-content: space-between;
    gap: 12px;
    padding: 6px 0;
    border-bottom: 1px dashed var(--seg-off);
  }

  .clabel {
    color: var(--muted);
  }

  .cval {
    display: inline-flex;
    align-items: flex-start;
    gap: 8px;
    text-align: right;
  }

  .dot {
    flex: none;
    display: inline-block;
    width: 8px;
    height: 8px;
    margin-top: 6px;
    background: currentColor;
  }

  .fail-top {
    display: flex;
    flex-wrap: wrap;
    align-items: flex-end;
    justify-content: space-between;
    gap: 16px 32px;
  }

  .big {
    margin: 0;
    font-family: var(--font-display);
    font-size: 36px;
    line-height: 1;
  }

  .big-wrap p.small {
    margin: 4px 0 0;
  }

  .bars {
    display: flex;
    gap: 6px;
    align-items: flex-end;
    min-width: 240px;
    flex: 1 1 280px;
    max-width: 460px;
  }

  .bar-col {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 3px;
    min-width: 0;
  }

  .bar {
    display: flex;
    align-items: flex-end;
    width: 100%;
    height: 56px;
    background: var(--seg-off);
  }

  .bar i {
    display: block;
    width: 100%;
    background: var(--warn);
  }

  .two {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(340px, 1fr));
    gap: 14px;
    min-width: 0;
  }

  .table-wrap {
    overflow-x: auto;
  }

  .table-wrap.scroll {
    max-height: 360px;
    overflow-y: auto;
  }

  .tbl {
    width: 100%;
    border-collapse: collapse;
  }

  .tbl th {
    padding: 6px 10px;
    text-align: left;
    font-weight: 400;
    font-size: 12px;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--muted);
    border-bottom: 2px solid var(--line);
  }

  .tbl td {
    padding: 8px 10px;
    vertical-align: middle;
    border-bottom: 1px dashed var(--seg-off);
  }

  .num {
    text-align: right;
  }

  .fp {
    overflow-wrap: anywhere;
    max-width: 320px;
  }

  .plain {
    margin: 0;
    padding: 0;
    list-style: none;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .jail p {
    margin: 0 0 6px;
  }

  .ips {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }

  .chip-ip {
    padding: 1px 6px;
    border: 2px solid var(--line);
    font-size: 12px;
  }

  @media (max-width: 600px) {
    .two {
      grid-template-columns: minmax(0, 1fr);
    }

    .checks {
      grid-template-columns: minmax(0, 1fr);
    }

    .tbl thead {
      position: absolute;
      width: 1px;
      height: 1px;
      overflow: hidden;
      clip: rect(0 0 0 0);
    }

    .tbl,
    .tbl tbody {
      display: block;
    }

    .tbl tr {
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      gap: 2px 12px;
      padding: 8px 4px;
      border-bottom: 1px dashed var(--seg-off);
    }

    .tbl td {
      padding: 0;
      border: 0;
    }

    .num {
      text-align: left;
    }
  }
</style>
