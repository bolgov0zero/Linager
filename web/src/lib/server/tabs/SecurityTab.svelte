<script lang="ts">
  import { untrack } from 'svelte';
  import { api, ApiError, type SecState, type ServerDetail } from '../../../api';
  import { locale, t, tr as T } from '../../../i18n';
  import { showToast } from '../../toast';
  import Modal from '../../Modal.svelte';
  import { isPhone } from '../../ui/viewport';
  import { formatDateTime } from '../format';
  import F2bCard from './F2bCard.svelte';
  import Fold from '../../ui/Fold.svelte';
  import SecAcc from './SecAcc.svelte';
  import { barHeights, groupChecks, hintKey, issueCount, keyLabel, postureChecks, worstTone, type Check, type Tone } from './sec-util';
  import { unavailableKind } from './svc';
  import './phone-c.css';

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

  // ---- ending sessions ----
  type Ending = { kind: 'session'; id: string; user: string; ip: string } | { kind: 'user'; user: string; count: number };
  let ending = $state<Ending | null>(null);
  let endBusy = $state(false);
  let endError = $state<string | null>(null);

  /** Users with more than one remote session get a button for all of them. */
  const multiUsers = $derived.by(() => {
    const counts = new Map<string, number>();
    for (const s of sec?.sessions ?? []) counts.set(s.user, (counts.get(s.user) ?? 0) + 1);
    return [...counts].filter(([, n]) => n > 1).map(([user, count]) => ({ user, count }));
  });

  async function confirmEnd(): Promise<void> {
    const e = ending;
    if (!e || endBusy) return;
    endBusy = true;
    endError = null;
    try {
      const res = await api.securityTerminate(server.id, e.kind === 'session' ? { session: e.id } : { user: e.user });
      ending = null;
      showToast(res.terminated.length > 1 ? t('sec.end.doneN', undefined, { n: res.terminated.length }) : t('sec.end.done'));
      await load(server.id);
    } catch (err) {
      if (err instanceof ApiError && err.serverMessage === 'no such remote session') {
        endError = t('sec.end.gone');
        await load(server.id); // the list was out of date
      } else {
        endError = t('sec.end.failed');
      }
    } finally {
      endBusy = false;
    }
  }

  // ---- banning an address from the list of failed attempts ----
  const bannedIps = $derived(new Set((sec?.fail2ban.bans ?? []).map((b) => b.ip)));
  const canBan = $derived(!!sec && sec.fail2ban.installed && sec.fail2ban.active);
  let banning = $state<string | null>(null);

  async function banFromList(ip: string): Promise<void> {
    if (banning) return;
    banning = ip;
    try {
      await api.securityBan(server.id, ip, 0);
      showToast(t('sec.f2b.banned.done', undefined, { ip }));
      await load(server.id);
    } catch (e) {
      showToast(e instanceof ApiError && e.serverMessage ? e.serverMessage : t('common.genericError'));
    } finally {
      banning = null;
    }
  }

  // ---- removing a key from authorized_keys ----
  type SecKey = SecState['keys'][number];
  let removing = $state<SecKey | null>(null);
  let rmBusy = $state(false);
  let rmError = $state<string | null>(null);

  /** The warning for the key about to be deleted: it may be the only way in for the user. */
  const rmWarn = $derived.by(() => {
    const k = removing;
    if (!k || !sec) return null;
    const mine = sec.keys.filter((x) => x.user === k.user).length;
    if (mine > 1) return null;
    return sec.ssh.password_auth === 'no' ? 'sec.keys.warnLockout' : 'sec.keys.warnLast';
  });

  async function confirmRemove(): Promise<void> {
    const k = removing;
    if (!k || rmBusy) return;
    rmBusy = true;
    rmError = null;
    try {
      await api.securityKeyRemove(server.id, { user: k.user, file: k.file, fingerprint: k.fingerprint });
      removing = null;
      showToast(t('sec.keys.removed'));
      await load(server.id);
    } catch (e) {
      rmError = e instanceof ApiError && e.serverMessage ? e.serverMessage : t('common.genericError');
    } finally {
      rmBusy = false;
    }
  }

  const checks = $derived(sec ? postureChecks(sec, server.sample) : []);
  const tone = $derived(worstTone(checks));
  const issues = $derived(issueCount(checks));
  const groups = $derived(groupChecks(checks));
  const verdictText = $derived(
    issues === 0 ? $T('sec.state.ok') : $T(`sec.state.issues.${new Intl.PluralRules($locale).select(issues)}`, { n: issues }),
  );
  // The folding sections (an accordion on a phone, a card with a header on a desktop), all closed at the start.
  const Section = $derived($isPhone ? SecAcc : Fold);
  let accOpen = $state({ sessions: false, logins: false, failures: false, f2b: false, keys: false });
  const f2bMeta = $derived(
    !sec ? '' : !sec.fail2ban.installed ? $T('sec.f2b.notInstalled') : !sec.fail2ban.active ? $T('sec.f2b.stoppedBadge') : $T('sec.f2b.running'),
  );
  const f2bTone = $derived<'ok' | 'warn'>(sec?.fail2ban.installed && sec.fail2ban.active ? 'ok' : 'warn');
  const heights = $derived(sec ? barHeights(sec.failures.by_day) : []);
  const unreachable = $derived(!server.online || loadState === 'offline' || loadState === 'disabled');

  /** Weekday label of the 24 hours bucket i (the last one ends now). */
  function dayLabel(i: number, count: number): string {
    if (!sec) return '';
    const end = new Date((sec.generated_at - (count - 1 - i) * 86400) * 1000);
    return end.toLocaleDateString($locale, { weekday: 'short' });
  }

  /** 8x8 pixel marks of the checks: a tick, an exclamation, a cross and a dash for the informational ones. */
  const MARKS: Record<Tone, readonly string[]> = {
    ok: ['00000000', '00000001', '00000011', '10000110', '11001100', '01111000', '00110000', '00000000'],
    warn: ['00011000', '00011000', '00011000', '00011000', '00011000', '00000000', '00011000', '00011000'],
    crit: ['10000001', '11000011', '01100110', '00111100', '00111100', '01100110', '11000011', '10000001'],
    muted: ['00000000', '00000000', '00000000', '01111110', '01111110', '00000000', '00000000', '00000000'],
  };

  const method = (m: string) => {
    const key = `sec.method.${m}`;
    const text = $T(key);
    return text === key ? m : text;
  };
</script>

{#snippet sessionsBlock(sec: SecState)}
    <section class="card stack" aria-labelledby="sec-sessions">
      <h3 id="sec-sessions">{$T('sec.sessions')}</h3>
      {#if sec.sessions.length === 0}
        <p class="muted">{$T('sec.sessions.none')}</p>
      {:else}
        {#if $isPhone}
          <ul class="pc-list">
            {#each sec.sessions as s (s.id)}
              <li class="pc-li">
                <div class="pc-mn">
                  <div class="pc-l1"><b class="mono">{s.user}</b></div>
                  <div class="pc-l2 mono">{[s.ip || '—', s.since ? formatDateTime(s.since, $locale) : ''].filter(Boolean).join(' · ')}</div>
                </div>
                <button class="btn sm" type="button" disabled={unreachable} onclick={() => { ending = { kind: 'session', id: s.id, user: s.user, ip: s.ip }; endError = null; }}>
                  {$T('sec.end')}
                </button>
              </li>
            {/each}
          </ul>
        {:else}
        <ul class="plain">
          {#each sec.sessions as s (s.id)}
            <li class="sess">
              <span>
                <span class="mono">{s.user}</span> · <span class="mono">{s.ip || '—'}</span>
                {#if s.since}<span class="muted small"> · {$T('sec.since', { time: formatDateTime(s.since, $locale) })}</span>{/if}
              </span>
              <button class="btn small-btn" type="button" disabled={unreachable} onclick={() => { ending = { kind: 'session', id: s.id, user: s.user, ip: s.ip }; endError = null; }}>
                {$T('sec.end')}
              </button>
            </li>
          {/each}
        </ul>
        {/if}
        {#each multiUsers as m (m.user)}
          <button class="btn small-btn all" type="button" disabled={unreachable} onclick={() => { ending = { kind: 'user', user: m.user, count: m.count }; endError = null; }}>
            {$T('sec.endAll', { user: m.user, n: m.count })}
          </button>
        {/each}
        {#if ending && !$isPhone}
          <div class="confirm stack" role="alertdialog" aria-label={$T('sec.end')}>
            <p>
              {ending.kind === 'session'
                ? $T('sec.end.confirm', { user: ending.user, ip: ending.ip || '—' })
                : $T('sec.end.confirmUser', { user: ending.user, n: ending.count })}
            </p>
            <p class="muted small">{$T('sec.end.hint')}</p>
            {#if endError}<p class="error" role="alert">{endError}</p>{/if}
            <div class="row end">
              <button class="btn" type="button" disabled={endBusy} onclick={() => (ending = null)}>{$T('common.cancel')}</button>
              <button class="btn danger" type="button" disabled={endBusy} onclick={() => void confirmEnd()}>{$T('sec.end')}</button>
            </div>
          </div>
        {/if}
      {/if}
    </section>
{/snippet}

{#snippet failuresBody(sec: SecState)}
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
        {#if $isPhone}
          <div class="pc-sech"><h4>{$T('sec.col.ip')}</h4></div>
          <ul class="pc-list">
            {#each sec.failures.top_ips as r (r.ip)}
              <li class="pc-li">
                <div class="pc-mn">
                  <div class="pc-l1"><b class="mono">{r.ip}</b></div>
                  <div class="pc-l2">{$T('sec.col.attempts')}: {r.count.toLocaleString($locale)} · {$T('sec.col.usersTried')}: {r.users}</div>
                  <div class="pc-l2">{formatDateTime(r.last, $locale)}</div>
                </div>
                {#if canBan}
                  {#if bannedIps.has(r.ip)}
                    <span class="pill">{$T('sec.f2b.bannedPill')}</span>
                  {:else if r.ip !== sec.self_ip}
                    <button class="btn sm" type="button" disabled={unreachable || banning !== null} onclick={() => void banFromList(r.ip)}>{$T('sec.f2b.ban')}</button>
                  {/if}
                {/if}
              </li>
            {/each}
          </ul>
          <div class="pc-sech"><h4>{$T('sec.col.user')}</h4></div>
          <ul class="pc-list">
            {#each sec.failures.top_users as r (r.user)}
              <li class="pc-li">
                <div class="pc-mn">
                  <div class="pc-l1"><b class="mono">{r.user || '—'}</b>{#if r.invalid}<span class="badge tone-muted">{$T('sec.badge.noUser')}</span>{/if}</div>
                </div>
                <span class="pc-r">{r.count.toLocaleString($locale)}</span>
              </li>
            {/each}
          </ul>
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
                  {#if canBan}<th scope="col" class="num"><span class="sr">{$T('sec.f2b.bans.actions')}</span></th>{/if}
                </tr>
              </thead>
              <tbody>
                {#each sec.failures.top_ips as r (r.ip)}
                  <tr>
                    <td class="mono">{r.ip}</td>
                    <td class="num">{r.count.toLocaleString($locale)}</td>
                    <td class="num">{r.users}</td>
                    <td class="small muted">{formatDateTime(r.last, $locale)}</td>
                    {#if canBan}
                      <td class="num">
                        {#if bannedIps.has(r.ip)}
                          <span class="pill">{$T('sec.f2b.bannedPill')}</span>
                        {:else if r.ip !== sec.self_ip}
                          <button class="btn small-btn" type="button" disabled={unreachable || banning !== null} onclick={() => void banFromList(r.ip)}>{$T('sec.f2b.ban')}</button>
                        {/if}
                      </td>
                    {/if}
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
      {/if}
{/snippet}

{#snippet loginsBody(sec: SecState)}
      {#if sec.logins.length === 0}
        <p class="muted">{$T('sec.logins.none')}</p>
      {:else}
        {#if $isPhone}
          <ul class="pc-list">
            {#each sec.logins as l, i (i)}
              <li class="pc-li">
                <div class="pc-mn">
                  <div class="pc-l1"><b class="mono">{l.user}</b> <span class="mono">{l.ip}</span></div>
                  <div class="pc-l2">{formatDateTime(l.time, $locale)} · {method(l.method)}</div>
                </div>
                {#if l.prior_failures > 0}
                  <span class="badge tone-warn">{$T('sec.badge.prior', { n: l.prior_failures })}</span>
                {/if}
              </li>
            {/each}
          </ul>
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
      {/if}
{/snippet}

{#snippet keysBody(sec: SecState)}
      <p class="muted small">{$T('sec.keys.hint')}</p>
      {#if sec.keys.length === 0}
        <p class="muted">{$T('sec.keys.none')}</p>
      {:else}
        {#if $isPhone}
          <ul class="pc-list">
            {#each sec.keys as k, i (i)}
              <li class="pc-li">
                <div class="pc-mn">
                  <div class="pc-l1">
                    <b class="mono">{k.user}</b>
                    <span>{keyLabel(k.type, k.bits)}</span>
                    {#if k.weak}<span class="badge tone-crit">{$T('sec.badge.weak')}</span>{/if}
                    {#if k.restricted}<span class="badge tone-muted">{$T('sec.badge.restricted')}</span>{/if}
                  </div>
                  <div class="pc-l2 mono">{k.fingerprint}</div>
                  <div class="pc-l2">{[k.comment || '—', k.mtime ? formatDateTime(k.mtime, $locale) : ''].filter(Boolean).join(' · ')}</div>
                </div>
                <button class="btn sm" type="button" disabled={unreachable} onclick={() => { removing = k; rmError = null; }}>{$T('sec.keys.remove')}</button>
              </li>
            {/each}
          </ul>
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
                <th scope="col" class="num"><span class="sr">{$T('sec.f2b.bans.actions')}</span></th>
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
                  <td class="num">
                    <button class="btn small-btn" type="button" disabled={unreachable} onclick={() => { removing = k; rmError = null; }}>{$T('sec.keys.remove')}</button>
                  </td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
        {/if}
        {#if removing && !$isPhone}
          <div class="confirm stack" role="alertdialog" aria-label={$T('sec.keys.remove')}>
            <p>{$T('sec.keys.removeText', { user: removing.user, fp: removing.fingerprint })}</p>
            {#if rmWarn}<p class="error">{$T(rmWarn, { user: removing.user })}</p>{/if}
            <p class="muted small">{$T('sec.keys.removeNote')}</p>
            {#if rmError}<p class="error" role="alert">{rmError}</p>{/if}
            <div class="row end">
              <button class="btn" type="button" disabled={rmBusy} onclick={() => (removing = null)}>{$T('common.cancel')}</button>
              <button class="btn danger" type="button" disabled={rmBusy} onclick={() => void confirmRemove()}>{$T('sec.keys.removeGo')}</button>
            </div>
          </div>
        {/if}
      {/if}
{/snippet}

{#snippet checkRow(c: Check)}
  {@const hint = hintKey(c)}
  <li class="crow">
    <svg class="mark tone-{c.tone}" viewBox="0 0 8 8" width="20" height="20" fill="currentColor" shape-rendering="crispEdges" aria-hidden="true">
      {#each MARKS[c.tone] as row, y (y)}
        {#each [...row] as cell, x (x)}
          {#if cell === '1'}<rect {x} {y} width="1" height="1" />{/if}
        {/each}
      {/each}
    </svg>
    <div class="cbody">
      <div class="cline">
        <span class="clabel">{$T(c.label)}</span>
        <span class="cval tone-{c.tone}">{$T(c.value, c.vars)}</span>
      </div>
      {#if hint}<p class="chint muted small">{$T(hint)}</p>{/if}
    </div>
  </li>
{/snippet}

{#snippet stateBlock()}
  <section class="card stack" aria-labelledby="sec-state">
    <h3 id="sec-state">{$T('sec.state')}</h3>
    <p class="verdict tone-{tone}" role="status">{verdictText}</p>
    {#if groups.ssh.length > 0}
      <h4 class="grp">{$T('sec.state.groupSsh')}</h4>
      <ul class="checks">
        {#each groups.ssh as c (c.id)}{@render checkRow(c)}{/each}
      </ul>
    {/if}
    {#if groups.system.length > 0}
      <h4 class="grp">{$T('sec.state.groupSystem')}</h4>
      <ul class="checks">
        {#each groups.system as c (c.id)}{@render checkRow(c)}{/each}
      </ul>
    {/if}
  </section>
{/snippet}

<div class="sec">
  {#if unreachable}
    <p class="notice" role="status">{$T(loadState === 'disabled' ? 'svc.disabled' : 'svc.offline')}</p>
  {/if}

  {#if !$isPhone}
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
  {/if}

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

    {#if $isPhone}
      <div class="card sumcard">
        <div class="srow">
          <div class="smain muted small">
            {[$T('sec.period', { days: sec.period_days ?? 7 }), $T('sec.updated', { time: formatDateTime(sec.generated_at, $locale) })].join(' · ')}
          </div>
          <button class="btn" type="button" disabled={busy || unreachable} onclick={() => void load(server.id)}>{$T('sec.refresh')}</button>
        </div>
      </div>
    {/if}

    {@render stateBlock()}

    {#if $isPhone}
      <SecAcc title={$T('sec.sessions')} meta={String(sec.sessions.length)} tone={sec.sessions.length > 0 ? 'warn' : 'muted'} bind:open={accOpen.sessions}>
        {@render sessionsBlock(sec)}
      </SecAcc>
      <SecAcc title="fail2ban" meta={f2bMeta} tone={f2bTone} bind:open={accOpen.f2b}>
        <F2bCard serverId={server.id} {sec} {unreachable} onchanged={() => load(server.id)} />
      </SecAcc>
    {:else}
      {@render sessionsBlock(sec)}
      <F2bCard serverId={server.id} {sec} {unreachable} onchanged={() => load(server.id)} />
    {/if}

    <Section title={$T('sec.failures')} meta={sec.failures.total.toLocaleString($locale)} tone={sec.failures.total > 0 ? 'warn' : 'muted'} bind:open={accOpen.failures}>
      {#if $isPhone}<section class="card stack">{@render failuresBody(sec)}</section>{:else}{@render failuresBody(sec)}{/if}
    </Section>
    <Section title={$T('sec.logins')} meta={String(sec.logins.length)} bind:open={accOpen.logins}>
      {#if $isPhone}<section class="card stack">{@render loginsBody(sec)}</section>{:else}{@render loginsBody(sec)}{/if}
    </Section>
    <Section title={$T('sec.keys')} meta={String(sec.keys.length)} bind:open={accOpen.keys}>
      {#if $isPhone}<section class="card stack">{@render keysBody(sec)}</section>{:else}{@render keysBody(sec)}{/if}
    </Section>
  {/if}
</div>

{#if ending && $isPhone}
  <Modal title={$T('mobC.sec.endTitle')} labelId="sec-end-title" onclose={() => (ending = null)}>
    <p>
      {ending.kind === 'session'
        ? $T('sec.end.confirm', { user: ending.user, ip: ending.ip || '—' })
        : $T('sec.end.confirmUser', { user: ending.user, n: ending.count })}
    </p>
    <p class="muted small">{$T('sec.end.hint')}</p>
    {#if endError}<p class="error" role="alert">{endError}</p>{/if}
    <div class="row end">
      <button class="btn" type="button" disabled={endBusy} onclick={() => (ending = null)}>{$T('common.cancel')}</button>
      <button class="btn danger" type="button" disabled={endBusy} onclick={() => void confirmEnd()}>{$T('sec.end')}</button>
    </div>
  </Modal>
{/if}

{#if removing && $isPhone}
  <Modal title={$T('sec.keys.remove')} labelId="sec-rm-title" onclose={() => (removing = null)}>
    <p>{$T('sec.keys.removeText', { user: removing.user, fp: removing.fingerprint })}</p>
    {#if rmWarn}<p class="error">{$T(rmWarn, { user: removing.user })}</p>{/if}
    <p class="muted small">{$T('sec.keys.removeNote')}</p>
    {#if rmError}<p class="error" role="alert">{rmError}</p>{/if}
    <div class="row end">
      <button class="btn" type="button" disabled={rmBusy} onclick={() => (removing = null)}>{$T('common.cancel')}</button>
      <button class="btn danger" type="button" disabled={rmBusy} onclick={() => void confirmRemove()}>{$T('sec.keys.removeGo')}</button>
    </div>
  </Modal>
{/if}

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

  .verdict {
    margin: 0;
    font-family: var(--font-display);
    font-size: 22px;
    line-height: 1.15;
  }

  .grp {
    margin: 4px 0 -4px;
    font-family: var(--font-display);
    font-size: 14px;
    font-weight: 400;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--muted);
  }

  .checks {
    display: flex;
    flex-direction: column;
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .crow {
    display: flex;
    align-items: flex-start;
    gap: 12px;
    padding: 9px 0;
    border-bottom: 1px dashed var(--seg-off);
  }

  .crow:last-child {
    border-bottom: 0;
  }

  .mark {
    flex: none;
    display: block;
    margin-top: 1px;
  }

  .cbody {
    flex: 1;
    min-width: 0;
  }

  .cline {
    display: flex;
    flex-wrap: wrap;
    justify-content: space-between;
    gap: 2px 16px;
  }

  .clabel {
    min-width: 0;
  }

  .cval {
    min-width: 0;
    margin-left: auto;
    text-align: right;
  }

  .chint {
    margin: 2px 0 0;
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

  .sess {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 6px 12px;
  }

  .small-btn {
    display: inline-flex;
    align-items: center;
    height: 28px;
    padding: 0 8px;
    font-size: 13px;
  }

  .all {
    align-self: flex-start;
  }

  .confirm {
    padding: 12px;
    border: 2px solid var(--crit);
  }

  .confirm p {
    margin: 0;
  }


  .ips {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }

  .pill {
    display: inline-flex;
    align-items: center;
    height: 28px;
    padding: 0 10px;
    border: 2px solid var(--crit);
    color: var(--crit);
    font-size: 13px;
    white-space: nowrap;
  }

  .sr {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip: rect(0 0 0 0);
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

  /* Phone: a summary card, the marks as rows, accordion sections. */
  @media (max-width: 720px) {
    .sec {
      gap: 12px;
    }

    .srow {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 10px;
    }

    .smain {
      min-width: 0;
    }

    .cline {
      flex-direction: column;
      gap: 0;
    }

    .cval {
      margin-left: 0;
      text-align: left;
    }

    h4 {
      margin: 0;
      font-family: var(--font-display);
      font-size: 14px;
      font-weight: 400;
      letter-spacing: 0.06em;
      text-transform: uppercase;
      color: var(--muted);
    }

    .grp {
      margin: 4px 0 -4px;
    }

    .pc-l1 .badge {
      font-size: 11.5px;
    }

    .pill {
      height: 28px;
    }
  }
</style>
