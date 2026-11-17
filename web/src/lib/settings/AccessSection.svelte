<script lang="ts">
  import { onMount } from 'svelte';
  import {
    api,
    isUnauthorized,
    type AccessCheck,
    type AccessConfig,
    type AccessHost,
    type AccessLogFilter,
    type AccessLogRow,
    type AccessRule,
    type AccessState,
    ApiError,
  } from '../../api';
  import { locale, t, tr as trans } from '../../i18n';
  import { showToast } from '../toast';
  import Fold from '../ui/Fold.svelte';
  import Select from '../ui/Select.svelte';

  let { onauthlost }: { onauthlost: () => void } = $props();

  const FILTERS: AccessLogFilter[] = ['all', 'denied', 'login', 'fail', 'proxy', 'direct'];
  const RFC1918 = ['10.0.0.0/8', '172.16.0.0/12', '192.168.0.0/16'];

  let srv = $state<AccessState | null>(null);
  let draft = $state<AccessConfig | null>(null);
  let check = $state<AccessCheck | null>(null);
  let loaded = $state(false);
  let busy = $state(false);
  let confirming = $state(false);
  let error = $state('');

  let openAccess = $state(true);
  let openProxy = $state(false);
  let openLog = $state(false);

  let newAddr = $state('');
  let newNote = $state('');
  let addErr = $state('');
  let newProxy = $state('');
  let newProxyNote = $state('');
  let proxyErr = $state('');

  let testIp = $state('');
  let testHow = $state('proxy');

  let hostBusy = $state(false);
  let refreshTick = $state(0); // a name was refreshed: ask the server about the draft again
  let now = $state(Date.now()); // the "N min ago" of the host names

  let filter = $state<AccessLogFilter>('all');
  let rows = $state<AccessLogRow[]>([]);
  let moreRows = $state(false);
  let retention = $state('30');

  const listKey = $derived(draft?.mode === 'black' ? 'black' : 'white');
  const list = $derived(draft ? draft[listKey] : []);
  const dirty = $derived(!!srv && !!draft && JSON.stringify(draft) !== JSON.stringify(srv.config));
  const problems = $derived(check?.problems ?? []);
  const trustAll = $derived(!!draft && draft.proxies.some((p) => p.cidr.endsWith('/0')));
  const hosts = $derived<Record<string, AccessHost>>({ ...(srv?.hosts ?? {}), ...(check?.hosts ?? {}) });
  const hasHosts = $derived(list.some((r) => isHostRule(r)));

  function fail(e: unknown): void {
    if (isUnauthorized(e)) onauthlost();
    else error = t('access.failed');
  }

  function clone<T>(v: T): T {
    return JSON.parse(JSON.stringify(v)) as T;
  }

  // ---- addresses ----

  function ip4(s: string): number | null {
    const p = s.split('.');
    if (p.length !== 4) return null;
    let n = 0;
    for (const part of p) {
      if (!/^\d{1,3}$/.test(part) || Number(part) > 255) return null;
      n = n * 256 + Number(part);
    }
    return n;
  }

  /** A valid address or network in the form the server stores ("1.2.3.4" becomes "1.2.3.4/32"), or null. */
  function normalize(raw: string): string | null {
    const s = raw.trim();
    if (!s) return null;
    const [addr, bits, extra] = s.split('/');
    if (extra !== undefined) return null;
    if (addr.includes(':')) {
      if (!/^[0-9a-f:]+$/i.test(addr) || (bits !== undefined && !(/^\d{1,3}$/.test(bits) && Number(bits) <= 128))) return null;
      return `${addr}/${bits ?? 128}`;
    }
    if (ip4(addr) === null || (bits !== undefined && !(/^\d{1,2}$/.test(bits) && Number(bits) <= 32))) return null;
    return `${addr}/${bits ?? 32}`;
  }

  /** A valid host name in the form the server stores (lower case, no final dot), or null. A name has a dot and does not end in a number. */
  function hostName(raw: string): string | null {
    const s = raw.trim().replace(/\.$/, '').toLowerCase();
    if (!s || s.length > 253 || !s.includes('.')) return null;
    const labels = s.split('.');
    if (!labels.every((l) => /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/.test(l))) return null;
    return /^\d/.test(labels[labels.length - 1]) ? null : s;
  }

  function isHostRule(rule: AccessRule): boolean {
    return normalize(rule.cidr) === null && hostName(rule.cidr) !== null;
  }

  /** Whether the network of a list holds the address (IPv4; other families only on an exact match). */
  function holds(cidr: string, addr: string): boolean {
    const [net, bits] = cidr.split('/');
    const a = ip4(addr);
    const n = ip4(net);
    if (a === null || n === null) return net === addr && Number(bits) === (addr.includes(':') ? 128 : 32);
    const len = Number(bits);
    if (len === 0) return true;
    const mask = (0xffffffff << (32 - len)) >>> 0;
    return ((a & mask) >>> 0) === ((n & mask) >>> 0);
  }

  function addTo(target: AccessRule[], cidr: string, note: string): string {
    const norm = normalize(cidr);
    if (!norm) return 'addr';
    if (target.some((r) => r.cidr === norm)) return 'dup';
    target.push({ cidr: norm, note: note.trim() });
    return '';
  }

  // ---- loading ----

  async function loadLog(more = false): Promise<void> {
    try {
      const before = more && rows.length ? rows[rows.length - 1].id : 0;
      const res = await api.accessLog(filter, before);
      rows = more ? [...rows, ...res.rows] : res.rows;
      moreRows = res.rows.length >= 50;
    } catch (e) {
      fail(e);
    }
  }

  async function load(): Promise<void> {
    try {
      srv = await api.access();
      draft = clone(srv.config);
      retention = String(srv.config.retention_days);
      await loadLog();
    } catch (e) {
      fail(e);
    } finally {
      loaded = true;
    }
  }

  onMount(() => {
    void load();
    const timer = setInterval(() => (now = Date.now()), 15000);
    return () => clearInterval(timer);
  });

  // The server judges the draft (what happens to you, whether it may be applied, what happens to the tested address).
  let seq = 0;
  $effect(() => {
    if (!draft) return;
    const body = $state.snapshot(draft);
    void refreshTick;
    const ip = testIp.trim();
    const test = ip ? { ip, proxy: testHow === 'proxy' } : undefined;
    const timer = setTimeout(async () => {
      const mine = ++seq;
      try {
        const res = await api.accessCheck(body, test);
        if (mine === seq) check = res;
      } catch (e) {
        if (isUnauthorized(e)) onauthlost();
      }
    }, 200);
    return () => clearTimeout(timer);
  });

  // ---- actions ----

  function setMode(mode: AccessConfig['mode']): void {
    if (draft) draft.mode = mode;
    addErr = '';
  }

  async function addRule(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    if (!draft || hostBusy) return;
    const host = normalize(newAddr) === null ? hostName(newAddr) : null;
    if (host) {
      // a name is added only when DNS knows it: the server looks it up
      if (draft[listKey].some((r) => r.cidr === host)) {
        addErr = t('access.err.dupHost');
        return;
      }
      hostBusy = true;
      try {
        await api.accessResolve(host);
      } catch (e) {
        if (isUnauthorized(e)) onauthlost();
        else addErr = e instanceof ApiError && e.status === 422 ? t('access.err.unresolved', $locale, { host }) : t('access.failed');
        return;
      } finally {
        hostBusy = false;
      }
      draft[listKey].push({ cidr: host, note: newNote.trim() });
      addErr = newAddr = newNote = '';
      return;
    }
    const res = addTo(draft[listKey], newAddr, newNote);
    addErr = res ? t(`access.err.${res}`) : '';
    if (!res) {
      newAddr = '';
      newNote = '';
    }
  }

  function addRfc(): void {
    if (!draft) return;
    for (const c of RFC1918) addTo(draft.white, c, t('access.rfc.note'));
  }

  function removeRule(index: number): void {
    draft?.[listKey].splice(index, 1);
  }

  /** Looks the name up again at once (the server also does it every minute). */
  async function refreshHost(host: string): Promise<void> {
    if (hostBusy) return;
    hostBusy = true;
    try {
      const res = await api.accessResolve(host);
      refreshTick++;
      if (srv) srv.hosts[host] = res.status;
      showToast(res.status.failing ? t('access.host.stillDown', $locale, { host }) : t('access.host.refreshed', $locale, { host, ips: res.status.ips.join(', ') }));
    } catch (e) {
      if (isUnauthorized(e)) onauthlost();
      else showToast(t('access.err.unresolved', $locale, { host }));
    } finally {
      hostBusy = false;
    }
  }

  function addProxy(event: SubmitEvent): void {
    event.preventDefault();
    if (!draft) return;
    if (normalize(newProxy)?.endsWith('/0')) {
      proxyErr = t('access.proxy.err.all');
      return;
    }
    if (normalize(newProxy) === null && hostName(newProxy)) {
      proxyErr = t('access.proxy.err.host');
      return;
    }
    const res = addTo(draft.proxies, newProxy, newProxyNote);
    proxyErr = res ? t(`access.err.${res}`) : '';
    if (!res) {
      newProxy = '';
      newProxyNote = '';
    }
  }

  /** Trust the connection this very request came from (the proxy in front of the panel), drop "trust everybody". */
  function trustPeer(): void {
    if (!draft || !srv) return;
    draft.proxies = draft.proxies.filter((p) => !p.cidr.endsWith('/0'));
    addTo(draft.proxies, srv.you.remote_addr, '');
  }

  function toList(ip: string): void {
    if (!draft || draft.mode === 'off') return;
    const res = addTo(draft[listKey], ip, '');
    if (res === 'dup') showToast(t('access.log.exists'));
    else if (!res) {
      openAccess = true;
      showToast(t('access.log.added', $locale, { ip }));
    }
  }

  async function apply(): Promise<void> {
    if (!draft || busy) return;
    busy = true;
    error = '';
    try {
      srv = await api.accessSave($state.snapshot(draft));
      draft = clone(srv.config);
      confirming = false;
      showToast(t('access.applied'));
      await loadLog();
    } catch (e) {
      fail(e);
    } finally {
      busy = false;
    }
  }

  function revert(): void {
    if (srv) draft = clone(srv.config);
    confirming = false;
    addErr = proxyErr = '';
  }

  async function setRetention(value: string): Promise<void> {
    try {
      srv = await api.accessRetention(Number(value));
      if (draft) draft.retention_days = srv.config.retention_days;
      showToast(t('access.log.retentionSaved', $locale, { n: value }));
    } catch (e) {
      fail(e);
    }
  }

  async function setFilter(f: AccessLogFilter): Promise<void> {
    filter = f;
    await loadLog();
  }

  // ---- text ----

  function reasonText(code: string): string {
    return t(`access.reason.${code}`);
  }

  function problemText(p: { code: string; ip?: string; reason?: string; host?: string }): string {
    return t(`access.problem.${p.code}`, $locale, { ip: p.ip ?? '', host: p.host ?? '', reason: p.reason ? reasonText(p.reason) : '' });
  }

  function ago(ts: number): string {
    const s = Math.max(0, Math.round((now - ts * 1000) / 1000));
    if (s < 60) return t('access.ago.s', $locale, { n: s });
    if (s < 3600) return t('access.ago.m', $locale, { n: Math.floor(s / 60) });
    return t('access.ago.h', $locale, { n: Math.floor(s / 3600) });
  }

  /** The line under the addresses of a name: when they were checked, or what is wrong. */
  function hostLine(h: AccessHost | undefined): { text: string; tone: 'ok' | 'warn' } {
    if (!h || !h.checked_at) return { text: t('access.host.never'), tone: 'warn' };
    if (h.expired) return { text: t('access.host.expired', $locale, { ago: ago(h.checked_at) }), tone: 'warn' };
    if (h.failing) return { text: t('access.host.failing', $locale, { ago: ago(h.checked_at) }), tone: 'warn' };
    if (now - h.checked_at * 1000 < 5000) return { text: t('access.host.okNow'), tone: 'ok' };
    return { text: t('access.host.ok', $locale, { ago: ago(h.checked_at) }), tone: 'ok' };
  }

  function formatTime(ts: number): string {
    return new Date(ts * 1000).toLocaleString($locale === 'en' ? 'en-GB' : 'ru-RU', {
      day: '2-digit',
      month: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
    });
  }

  const accessMeta = $derived(
    !srv ? '' : srv.config.mode === 'off' ? t('access.meta.off') : t(`access.meta.${srv.config.mode}`, $locale, { n: srv.config[srv.config.mode].length }),
  );
  const proxyMeta = $derived(
    !srv ? '' : srv.config.proxies.some((p) => p.cidr.endsWith('/0')) ? t('access.proxy.metaAll') : srv.config.proxies.length ? t('access.proxy.metaN', $locale, { n: srv.config.proxies.length }) : t('access.proxy.metaNone'),
  );
  const proxyTone = $derived(!srv ? 'muted' : srv.config.proxies.length === 0 || srv.config.proxies.some((p) => p.cidr.endsWith('/0')) ? 'warn' : 'ok');
  const peerListed = $derived(!!srv && !!draft && !trustAll && draft.proxies.some((p) => holds(p.cidr, srv!.you.remote_addr)));
  const peerUnlisted = $derived(!!srv && srv.you.via_proxy && !peerListed && !trustAll);
  const modeCount = $derived(draft && draft.mode !== 'off' ? draft[draft.mode].length : 0);
  const modeOptions = ['off', 'white', 'black'] as const;
  const retentionOptions = $derived(['7', '30', '90'].map((n) => ({ value: n, label: t('access.log.days', $locale, { n }) })));
  const howOptions = $derived([
    { value: 'proxy', label: t('access.test.proxy') },
    { value: 'direct', label: t('access.test.direct') },
  ]);
</script>

<div class="access">
  <h3 class="page">{$trans('access.title')}</h3>
  <p class="muted small">{$trans('access.lead')}</p>

  {#if !loaded}
    <p class="muted">{$trans('common.loading')}</p>
  {:else if srv && draft}
    {#if error}<p class="error" role="alert">{error}</p>{/if}

    <Fold title={$trans('access.restrict')} meta={accessMeta} tone={srv.config.mode === 'off' ? 'muted' : 'ok'} bind:open={openAccess}>
      <div class="radios" role="radiogroup" aria-label={$trans('access.mode.label')}>
        {#each modeOptions as m (m)}
          <label class="opt" class:on={draft.mode === m}>
            <input type="radio" name="access-mode" value={m} checked={draft.mode === m} onchange={() => setMode(m)} />
            <span>{$trans(`access.mode.${m}`)}<small>{$trans(`access.mode.${m}.hint`)}</small></span>
          </label>
        {/each}
      </div>

      {#if draft.mode !== 'off'}
        <div class="sec">
          <h4>{$trans(`access.list.${listKey}`, { n: list.length })}</h4>
          {#if list.length}
            <div class="wrap">
              <table class="tbl">
                <thead><tr><th>{$trans('access.col.addrHost')}</th><th>{$trans('access.col.now')}</th><th>{$trans('access.col.note')}</th><th></th></tr></thead>
                <tbody>
                  {#each list as rule, i (rule.cidr)}
                    {@const host = isHostRule(rule)}
                    {@const info = hosts[rule.cidr]}
                    {@const line = host ? hostLine(info) : null}
                    <tr>
                      <td class="nw mono">{rule.cidr}{#if host}<span class="ddns" title={$trans('access.host.badgeHint')}>DDNS</span>{/if}</td>
                      <td>
                        {#if host && line}
                          <div class="ips mono">{#each info?.ips ?? [] as ip (ip)}<div>{ip}</div>{/each}</div>
                          <div class="small {line.tone}">{line.text}</div>
                        {:else}
                          <span class="muted">—</span>
                        {/if}
                      </td>
                      <td>{rule.note || '—'}</td>
                      <td class="end nw">
                        {#if host}<button class="btn sm" type="button" disabled={hostBusy} aria-label={`${$trans('access.host.refresh')} ${rule.cidr}`} onclick={() => void refreshHost(rule.cidr)}>{$trans('access.host.refresh')}</button>{' '}{/if}
                        <button class="btn sm" type="button" aria-label={`${$trans('access.remove')} ${rule.cidr}`} onclick={() => removeRule(i)}>{$trans('access.remove')}</button>
                      </td>
                    </tr>
                  {/each}
                </tbody>
              </table>
            </div>
            {#if hasHosts}<div class="box info"><span>{$trans('access.host.note')}</span></div>{/if}
          {:else}
            <p class="muted small">{$trans('access.empty')}</p>
          {/if}
          <form class="row add" autocomplete="off" onsubmit={addRule}>
            <input class="input grow addr" placeholder={$trans('access.addr.ph')} aria-label={$trans('access.addr.aria')} bind:value={newAddr} />
            <input class="input grow" placeholder={$trans('access.note.ph')} aria-label={$trans('access.note.ph')} maxlength="100" bind:value={newNote} />
            <button class="btn primary" type="submit" disabled={hostBusy}>{$trans('access.add')}</button>
            {#if draft.mode === 'white'}<button class="btn" type="button" onclick={addRfc}>{$trans('access.rfc')}</button>{/if}
          </form>
          {#if addErr}<p class="error small" role="alert">{addErr}</p>{/if}
        </div>

        <label class="switch" class:on={draft.only_proxy}>
          <input type="checkbox" role="switch" bind:checked={draft.only_proxy} />
          <span>{$trans('access.onlyProxy')}<small>{$trans('access.onlyProxy.hint')}</small></span>
        </label>

        <div class="tester">
          <h4>{$trans('access.test.title')}</h4>
          <div class="row">
            <input class="input grow" placeholder="198.51.100.20" aria-label={$trans('access.test.aria')} bind:value={testIp} />
            <div class="how"><Select bind:value={testHow} options={howOptions} ariaLabel={$trans('access.test.how')} /></div>
          </div>
          <div class="small" aria-live="polite">
            {#if !testIp.trim()}
              <span class="muted">{$trans('access.test.enter')}</span>
            {:else if check?.test}
              <span class="badge" class:ok={check.test.allowed} class:crit={!check.test.allowed}>{$trans(check.test.allowed ? 'access.test.allowed' : 'access.test.denied')}</span>
              {check.test.ip} — {reasonText(check.test.reason)}{check.test.ignored_xff ? `; ${t('access.test.ignored')}` : ''}
            {/if}
          </div>
        </div>

        {#if check && problems.length === 0}
          <div class="box good"><p>{$trans('access.you.ok', { ip: check.you.ip, reason: reasonText(check.you.reason) })}</p></div>
        {/if}
      {/if}
    </Fold>

    <Fold title={$trans('access.proxy.title')} meta={proxyMeta} tone={proxyTone} bind:open={openProxy}>
      {#if trustAll}
        <div class="box warn"><p>{$trans('access.proxy.trustAll')}</p></div>
      {/if}
      {#if srv.you.via_proxy}
        <div class="box info">
          <span>{$trans('access.proxy.seen', { peer: srv.you.remote_addr, xff: srv.you.forwarded_for })}</span>
          {#if peerListed}
            <span class="ok">{$trans('access.proxy.listed')}</span>
          {:else}
            <button class="btn sm primary" type="button" onclick={trustPeer}>{$trans('access.proxy.addPeer', { peer: srv.you.remote_addr })}</button>
          {/if}
        </div>
      {:else}
        <div class="box info"><span>{$trans('access.proxy.direct', { peer: srv.you.remote_addr })}</span></div>
      {/if}
      {#if peerUnlisted}
        <div class="box crit"><p>{$trans('access.proxy.unlisted', { peer: srv.you.remote_addr })}</p></div>
      {/if}

      <div class="sec">
        <h4>{$trans('access.proxy.list', { n: draft.proxies.length })}</h4>
        {#if draft.proxies.length}
          <div class="wrap">
            <table class="tbl">
              <thead><tr><th>{$trans('access.col.addr')}</th><th>{$trans('access.col.note')}</th><th></th></tr></thead>
              <tbody>
                {#each draft.proxies as rule, i (rule.cidr)}
                  <tr>
                    <td class="nw mono">{rule.cidr}</td>
                    <td>{rule.note || '—'}</td>
                    <td class="end"><button class="btn sm" type="button" aria-label={`${$trans('access.remove')} ${rule.cidr}`} onclick={() => draft?.proxies.splice(i, 1)}>{$trans('access.remove')}</button></td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
        {:else}
          <p class="muted small">{$trans('access.proxy.empty')}</p>
        {/if}
        <form class="row add" autocomplete="off" onsubmit={addProxy}>
          <input class="input grow" placeholder={$trans('access.proxy.addr.ph')} aria-label={$trans('access.addr.aria')} bind:value={newProxy} />
          <input class="input grow" placeholder={$trans('access.note.ph')} aria-label={$trans('access.note.ph')} maxlength="100" bind:value={newProxyNote} />
          <button class="btn primary" type="submit">{$trans('access.add')}</button>
        </form>
        {#if proxyErr}<p class="error small" role="alert">{proxyErr}</p>{/if}
      </div>
    </Fold>

    <Fold
      title={$trans('access.log.title')}
      meta={srv.denied_24h ? t('access.log.metaDenied', $locale, { n: srv.denied_24h }) : t('access.log.metaNone')}
      tone={srv.denied_24h ? 'warn' : 'muted'}
      bind:open={openLog}>
      <div class="row">
        {#each FILTERS as f (f)}
          <button class="chip" type="button" aria-pressed={filter === f} onclick={() => void setFilter(f)}>{$trans(`access.log.f.${f}`)}</button>
        {/each}
        <span class="ret row small muted">
          {$trans('access.log.retention')}
          <Select size="sm" bind:value={retention} options={retentionOptions} ariaLabel={$trans('access.log.retention')} onchange={(v) => void setRetention(v)} />
        </span>
      </div>
      <div class="wrap">
        <table class="tbl wide">
          <thead>
            <tr>
              <th>{$trans('access.log.col.time')}</th><th>{$trans('access.log.col.client')}</th><th>{$trans('access.log.col.how')}</th>
              <th>{$trans('access.log.col.event')}</th><th>{$trans('access.log.col.login')}</th><th></th>
            </tr>
          </thead>
          <tbody>
            {#each rows as r (r.id)}
              <tr class:den={r.event === 'denied'}>
                <td class="nw">{formatTime(r.ts)}</td>
                <td class="nw mono">{r.client_ip}</td>
                <td class="nw">
                  {#if r.via_proxy}
                    {$trans('access.log.via', { proxy: r.remote_addr })}
                    {#if !r.proxy_trusted}<span class="badge warn" title={$trans('access.log.untrustedHint')}>{$trans('access.log.untrusted')}</span>{/if}
                  {:else}
                    {$trans('access.log.direct')}
                  {/if}
                </td>
                <td class="nw">
                  <span class="badge" class:ok={r.event === 'login.ok'} class:warn={r.event === 'login.fail'} class:crit={r.event === 'denied'} class:muted={r.event === 'page'}>{$trans(`access.log.e.${r.event}`)}</span>
                </td>
                <td>{r.login || '—'}</td>
                <td class="end">
                  {#if srv.config.mode !== 'off'}
                    <button class="btn sm" type="button" onclick={() => toList(r.client_ip)}>{$trans(srv.config.mode === 'black' ? 'access.log.toBlack' : 'access.log.toWhite')}</button>
                  {/if}
                </td>
              </tr>
            {:else}
              <tr><td colspan="6" class="muted">{$trans('access.log.empty')}</td></tr>
            {/each}
          </tbody>
        </table>
      </div>
      {#if moreRows}
        <div><button class="btn sm" type="button" onclick={() => void loadLog(true)}>{$trans('access.log.more')}</button></div>
      {/if}
      <p class="muted small">{$trans('access.log.foot')}</p>
    </Fold>

    {#if dirty}
      <div class="applybar" role="region" aria-live="polite">
        {#if confirming}
          <div class="msg">
            <b>{$trans('access.confirm.title')}</b>
            {$trans('access.confirm.text', { mode: $trans(`access.mode.${draft.mode}`), n: modeCount })}{draft.only_proxy && draft.mode !== 'off' ? $trans('access.confirm.onlyProxy') : ''}
            {#if check && draft.mode !== 'off'}<br />{$trans('access.confirm.you', { ip: check.you.ip })}{/if}
            <br /><span class="muted">{$trans('access.confirm.audit')}</span>
          </div>
          <button class="btn" type="button" disabled={busy} onclick={() => (confirming = false)}>{$trans('access.cancel')}</button>
          <button class="btn primary" type="button" disabled={busy} onclick={() => void apply()}>{$trans('access.confirm.ok')}</button>
        {:else}
          <div class="msg">
            {#if problems.length}
              <span class="crit">{$trans('access.cannotApply')}</span>
              {#each problems as p (p.code + (p.host ?? ''))}<br />{problemText(p)}{/each}
            {:else}
              {$trans('access.dirty')}
            {/if}
          </div>
          <button class="btn" type="button" onclick={revert}>{$trans('access.revert')}</button>
          <button class="btn primary" type="button" disabled={busy || problems.length > 0 || !check} onclick={() => (confirming = true)}>{$trans('access.apply')}</button>
        {/if}
      </div>
    {/if}
  {/if}
</div>

<style>
  .access {
    display: flex;
    flex-direction: column;
    gap: 14px;
    min-width: 0;
  }

  h3.page {
    margin: 0;
    font-size: 20px;
    font-weight: 400;
  }

  p {
    margin: 0;
  }

  .grow {
    flex: 1 1 160px;
    min-width: 0;
  }

  .grow.addr {
    flex: 2 1 280px;
  }

  .ddns {
    display: inline-block;
    margin-left: 6px;
    padding: 0 5px;
    border: 2px solid var(--accent);
    color: var(--accent);
    font-size: 11px;
    line-height: 1.3;
    vertical-align: middle;
  }

  .ips {
    font-size: 12px;
    line-height: 1.5;
  }

  .radios {
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

  .opt input {
    margin-top: 3px;
  }

  small {
    display: block;
    color: var(--muted);
    font-size: 12px;
  }

  .sec h4,
  .tester h4 {
    margin: 0 0 8px;
    font-size: 13px;
    font-weight: 400;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--muted);
  }

  .wrap {
    overflow-x: auto;
  }

  .tbl {
    width: 100%;
    border-collapse: collapse;
    font-size: 13px;
  }

  .tbl.wide {
    min-width: 720px;
  }

  .tbl th {
    padding: 6px 8px;
    border-bottom: 2px solid var(--line);
    font-family: var(--font-display);
    font-size: 12px;
    font-weight: 400;
    text-align: left;
    color: var(--muted);
    white-space: nowrap;
  }

  .tbl td {
    padding: 7px 8px;
    border-bottom: 1px dashed var(--seg-off);
    vertical-align: middle;
  }

  .tbl td.nw {
    white-space: nowrap;
  }

  .tbl td.end {
    text-align: right;
  }

  .tbl tr.den td {
    background: color-mix(in srgb, var(--crit) 8%, transparent);
  }

  .btn.sm {
    padding: 3px 8px;
    font-size: 12px;
  }

  .add {
    margin-top: 10px;
  }

  .switch {
    display: flex;
    align-items: flex-start;
    gap: 10px;
    cursor: pointer;
  }

  .switch input {
    margin-top: 2px;
  }

  .tester {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 10px;
    border: 2px dashed var(--seg-off);
  }

  .tester h4 {
    margin: 0;
  }

  .how {
    flex: 1 1 200px;
    min-width: 0;
  }

  .badge {
    display: inline-block;
    padding: 0 6px;
    border: 2px solid currentColor;
    font-size: 12px;
    line-height: 1.35;
    white-space: nowrap;
  }

  .box {
    padding: 8px 10px;
    border: 2px solid currentColor;
    font-size: 12px;
  }

  .box.good {
    color: var(--good);
  }

  .box.warn {
    color: var(--warn);
  }

  .box.crit {
    color: var(--crit);
  }

  .box.info {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    align-items: center;
    justify-content: space-between;
    border: 2px dashed var(--accent);
    color: var(--ink);
  }

  .ret {
    margin-left: auto;
    align-items: center;
  }

  .applybar {
    position: sticky;
    bottom: 0;
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 10px;
    padding: 10px 12px;
    background: var(--surface);
    border: 2px solid var(--accent);
    box-shadow: 4px 4px 0 var(--shade);
  }

  .applybar .msg {
    flex: 1 1 220px;
    font-size: 12px;
  }

  @media (max-width: 720px) {
    .applybar .btn {
      flex: 1;
    }

    .ret {
      margin-left: 0;
      width: 100%;
    }
  }
</style>
