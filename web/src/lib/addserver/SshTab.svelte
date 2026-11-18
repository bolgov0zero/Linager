<script lang="ts">
  import { onDestroy } from 'svelte';
  import { api, ApiError, isUnauthorized, type Group, type SSHState } from '../../api';
  import { locale, t, tr } from '../../i18n';
  import Fold from '../ui/Fold.svelte';
  import Select from '../ui/Select.svelte';
  import StageList from '../ui/StageList.svelte';
  import { showToast } from '../toast';
  import { failureCode, failureText, gateList, gateStr, isBlockedHost, pluralKey, sshStages } from './ssh';

  let {
    groups,
    onopen,
    onadded,
    onauthlost,
  }: {
    groups: Group[];
    /** Open the page of the new server (the dialog closes). */
    onopen: (name: string) => void;
    /** A server was installed: the dashboard may have changed. */
    onadded: () => void;
    onauthlost: () => void;
  } = $props();

  const FAST_MS = 700;
  const SLOW_MS = 2000;
  const OTHER = '';

  // The form. The secrets (password, key, passphrase, sudo password) exist only in this component: they are not
  // put in a URL, a log or the browser storage, and they are wiped when the install is over or the dialog closes.
  let host = $state('');
  let port = $state('22');
  let login = $state('');
  let auth = $state<'password' | 'key'>('password');
  let password = $state('');
  let key = $state('');
  let passphrase = $state('');
  let groupId = $state(0);

  let view = $state<'form' | 'session'>('form');
  let starting = $state(false);
  let startError = $state('');
  let st = $state<SSHState | null>(null);
  let expired = $state(false);
  let gateBusy = $state(false);

  // The answers to the gates.
  let sudoPw = $state('');
  let addrPick = $state(OTHER);
  let addrOther = $state('');
  let reinstallOk = $state(false);

  let logOpen = $state(false);
  let logEl = $state<HTMLElement | undefined>(undefined);

  let sessionId: string | null = null;
  let timer: ReturnType<typeof setTimeout> | undefined;
  let alive = true;

  const blocked = $derived(isBlockedHost(host));
  const portNum = $derived(Number(port));
  const portOk = $derived(Number.isInteger(portNum) && portNum >= 1 && portNum <= 65535);
  const canCheck = $derived(
    !blocked && !starting && host.trim() !== '' && login.trim() !== '' && portOk && (auth === 'password' ? password !== '' : key.trim() !== ''),
  );
  const groupOptions = $derived([
    { value: 0, label: $tr('asv.group.none') },
    ...groups.map((g) => ({ value: g.id, label: g.name })),
  ]);

  const phase = $derived(st?.phase);
  const installing = $derived(phase === 'installing' || phase === 'done' || phase === 'install_failed');
  const stages = $derived(st ? sshStages(st, installing, $tr) : []);
  const gate = $derived(st?.gate);
  const failure = $derived(st ? failureCode(st) : { code: '', detail: '' });
  const newName = $derived(st?.new_name ?? '');
  const logLines = $derived(st?.log ?? []);
  const logTotal = $derived(Math.max(st?.log_total ?? 0, logLines.length));

  const candidates = $derived(gateList(gate, 'candidates'));
  const addrOptions = $derived([
    ...candidates.map((c) => ({ value: c, label: c })),
    { value: OTHER, label: $tr('asv.ssh.gate.addrOther') },
  ]);
  const addrValue = $derived(addrPick === OTHER ? addrOther.trim() : addrPick);

  function wipeSecrets(): void {
    password = '';
    key = '';
    passphrase = '';
    sudoPw = '';
  }

  function stopPolling(): void {
    clearTimeout(timer);
    timer = undefined;
  }

  function schedule(ms: number): void {
    stopPolling();
    if (!alive || !sessionId) return;
    timer = setTimeout(() => void poll(), ms);
  }

  /** Forget the session: stop polling and tell the server to drop it (and the secrets it holds). */
  function closeSession(): void {
    stopPolling();
    const id = sessionId;
    sessionId = null;
    st = null;
    if (id) void api.sshClose(id).catch(() => {});
  }

  function gotExpired(): void {
    stopPolling();
    sessionId = null;
    st = null;
    expired = true;
    view = 'form';
  }

  function apply(next: SSHState): void {
    st = next;
    if (next.phase === 'done' || next.phase === 'closed') wipeSecrets();
    if (next.phase === 'done') {
      showToast(t('asv.ssh.doneToast', undefined, { name: next.new_name ?? next.host }));
      onadded();
    }
    if (next.phase === 'install_failed') logOpen = true;
    if (next.phase === 'done' || next.phase === 'install_failed' || next.phase === 'closed') {
      stopPolling();
      return;
    }
    schedule(next.phase === 'checking' || next.phase === 'installing' ? FAST_MS : SLOW_MS);
  }

  function onError(e: unknown): void {
    if (isUnauthorized(e)) onauthlost();
    else if (e instanceof ApiError && e.status === 404) gotExpired();
    else if (e instanceof ApiError && e.status === 409) schedule(0);
    else {
      showToast(t('common.genericError'));
      schedule(SLOW_MS);
    }
  }

  async function poll(): Promise<void> {
    const id = sessionId;
    if (!id) return;
    try {
      const next = await api.sshState(id);
      if (id === sessionId) apply(next);
    } catch (e) {
      if (id === sessionId) onError(e);
    }
  }

  async function start(): Promise<void> {
    if (!canCheck) return;
    starting = true;
    startError = '';
    expired = false;
    try {
      const res = await api.sshStart({
        host: host.trim(),
        port: portNum,
        login: login.trim(),
        auth,
        password: auth === 'password' ? password : '',
        key: auth === 'key' ? key : '',
        passphrase: auth === 'key' ? passphrase : '',
        group_id: groupId === 0 ? null : groupId,
      });
      sessionId = res.id;
      st = null;
      sudoPw = '';
      logOpen = false;
      view = 'session';
      schedule(0);
    } catch (e) {
      if (isUnauthorized(e)) {
        onauthlost();
      } else if (e instanceof ApiError && e.status === 429) {
        startError = t('asv.ssh.tooMany');
      } else if (e instanceof ApiError && e.status === 422) {
        startError = e.serverMessage ?? t('asv.ssh.startFailed');
      } else {
        startError = t('asv.ssh.startFailed');
      }
    } finally {
      starting = false;
    }
  }

  async function answer(accept: boolean, extra: { password?: string; address?: string } = {}): Promise<void> {
    const id = sessionId;
    const g = st?.gate;
    if (!id || !g || gateBusy) return;
    gateBusy = true;
    try {
      const next = await api.sshAnswer(id, { gate: g.kind, accept, ...extra });
      sudoPw = '';
      if (id === sessionId) apply(next);
    } catch (e) {
      if (id === sessionId) onError(e);
    } finally {
      gateBusy = false;
    }
  }

  async function install(): Promise<void> {
    const id = sessionId;
    if (!id || gateBusy) return;
    gateBusy = true;
    try {
      const next = await api.sshInstall(id);
      if (id === sessionId) apply(next);
    } catch (e) {
      if (id === sessionId) onError(e);
    } finally {
      gateBusy = false;
    }
  }

  /** Back to the form with what was typed (the session is dropped on the server). */
  function edit(): void {
    closeSession();
    view = 'form';
  }

  async function recheck(): Promise<void> {
    closeSession();
    view = 'form';
    await start();
  }

  /** "Add another": a clean form; nothing secret stays. */
  function again(): void {
    closeSession();
    wipeSecrets();
    host = '';
    view = 'form';
  }

  // A new question starts with empty answers; the sudo password of the SSH login is the first thing to try.
  let seenGate = '';
  $effect(() => {
    const kind = gate?.kind ?? '';
    const retry = gate?.data?.retry === true;
    const sig = `${kind}:${gateList(gate, 'candidates').join(',')}:${retry}`;
    if (sig === seenGate) return;
    seenGate = sig;
    sudoPw = kind === 'sudo' && !retry && auth === 'password' ? password : '';
    addrPick = candidates[0] ?? OTHER;
    addrOther = '';
    reinstallOk = false;
  });

  // The output stays scrolled to its end.
  $effect(() => {
    void logLines.length;
    void logOpen;
    if (logOpen && logEl) logEl.scrollTop = logEl.scrollHeight;
  });

  onDestroy(() => {
    alive = false;
    stopPolling();
    const id = sessionId;
    sessionId = null;
    if (id) void api.sshClose(id).catch(() => {});
    wipeSecrets();
  });
</script>

<section class="panel">
  {#if view === 'form'}
    <h3 class="page">{$tr('asv.tab.ssh')}</h3>
    <p class="lead">{$tr('asv.ssh.lead')}</p>
    {#if expired}<p class="box warn">{$tr('asv.ssh.expired')}</p>{/if}

    <form
      class="sec"
      autocomplete="off"
      onsubmit={(e) => {
        e.preventDefault();
        void start();
      }}
    >
      <h4>{$tr('asv.ssh.where')}</h4>
      <div class="card">
        <div class="grid">
          <label class="field">
            <span class="label">{$tr('asv.ssh.host')}</span>
            <input class="input mono" class:bad={blocked} bind:value={host} spellcheck="false" autocomplete="off" autocapitalize="off" />
          </label>
          <label class="field">
            <span class="label">{$tr('asv.ssh.port')}</span>
            <input class="input mono" class:bad={!portOk} bind:value={port} inputmode="numeric" autocomplete="off" />
          </label>
          <label class="field">
            <span class="label">{$tr('asv.ssh.login')}</span>
            <input class="input mono" bind:value={login} spellcheck="false" autocomplete="off" autocapitalize="off" />
          </label>
        </div>
        {#if blocked}<p class="crit small">{$tr('asv.ssh.blocked')}</p>{/if}

        <div class="row">
          <span class="muted small">{$tr('asv.ssh.auth')}</span>
          <button class="chip" type="button" aria-pressed={auth === 'password'} onclick={() => (auth = 'password')}>{$tr('asv.ssh.authPassword')}</button>
          <button class="chip" type="button" aria-pressed={auth === 'key'} onclick={() => (auth = 'key')}>{$tr('asv.ssh.authKey')}</button>
        </div>

        {#if auth === 'password'}
          <label class="field">
            <span class="label">{$tr('asv.ssh.password')}</span>
            <input class="input mono" type="password" bind:value={password} autocomplete="new-password" />
          </label>
        {:else}
          <label class="field">
            <span class="label">{$tr('asv.ssh.key')}</span>
            <textarea class="input mono" rows="4" bind:value={key} placeholder="-----BEGIN OPENSSH PRIVATE KEY-----" spellcheck="false" autocomplete="off"></textarea>
          </label>
          <label class="field">
            <span class="label">{$tr('asv.ssh.passphrase')}</span>
            <input class="input mono" type="password" bind:value={passphrase} autocomplete="new-password" />
          </label>
        {/if}

        <label class="field">
          <span class="label">{$tr('asv.ssh.group')}</span>
          <Select bind:value={groupId} options={groupOptions} />
        </label>

        <p class="box info">{$tr('asv.ssh.secrets')}</p>
        {#if startError}<p class="error small" role="alert">{startError}</p>{/if}
        <div class="row end">
          <button class="btn primary" type="submit" disabled={!canCheck}>{$tr('asv.ssh.check')}</button>
        </div>
      </div>
    </form>
  {:else}
    <h3 class="page">
      {#if installing}{$tr('asv.ssh.installTitle', { host: st?.host ?? host })}{:else}{$tr('asv.ssh.checkTitle', { target: `${st?.login ?? login}@${st?.host ?? host}` })}{/if}
    </h3>
    <div class="card">
      {#if !st}
        <p class="muted small">{$tr('asv.ssh.starting')}</p>
      {:else}
        <StageList {stages} nowMs={st.now_ms} label={$tr(installing ? 'asv.ssh.stagesInstall' : 'asv.ssh.stagesCheck')} />

        {#if gate?.kind === 'hostkey'}
          <div class="box warn">
            <p><strong>{$tr('asv.ssh.gate.hostkeyTitle')}</strong></p>
            <p>{$tr('asv.ssh.gate.hostkeyText', { type: gateStr(gate, 'type') })}</p>
            <p class="mono fp">{gateStr(gate, 'fingerprint')}</p>
            <p>{$tr('asv.ssh.gate.hostkeyWarn')}</p>
            <div class="row">
              <button class="btn sm primary" type="button" disabled={gateBusy} onclick={() => void answer(true)}>{$tr('asv.ssh.gate.trust')}</button>
              <button class="btn sm" type="button" disabled={gateBusy} onclick={() => void answer(false)}>{$tr('asv.ssh.gate.cancel')}</button>
            </div>
          </div>
        {:else if gate?.kind === 'sudo'}
          <form
            class="box info"
            onsubmit={(e) => {
              e.preventDefault();
              if (sudoPw) void answer(true, { password: sudoPw });
            }}
          >
            <p><strong>{$tr('asv.ssh.gate.sudoTitle')}</strong> {$tr('asv.ssh.gate.sudoText', { login: st.login })}</p>
            {#if gate.data?.retry === true}<p class="crit">{$tr('asv.ssh.gate.sudoRetry')}</p>{/if}
            <label class="field gatefield">
              <span class="label">{$tr('asv.ssh.gate.sudoField')}</span>
              <input class="input mono" type="password" bind:value={sudoPw} autocomplete="new-password" />
            </label>
            <div class="row">
              <button class="btn sm primary" type="submit" disabled={gateBusy || !sudoPw}>{$tr('asv.ssh.gate.continue')}</button>
              <button class="btn sm" type="button" disabled={gateBusy} onclick={() => void answer(false)}>{$tr('asv.ssh.gate.cancel')}</button>
            </div>
          </form>
        {:else if gate?.kind === 'address'}
          <div class="box warn">
            <p><strong>{$tr('asv.ssh.gate.addrTitle')}</strong></p>
            <p>{$tr('asv.ssh.gate.addrText', { port: gateStr(gate, 'port') })}</p>
            <div class="addr">
              <Select bind:value={addrPick} options={addrOptions} ariaLabel={$tr('asv.ssh.gate.addrOther')} />
              {#if addrPick === OTHER}
                <input class="input mono" bind:value={addrOther} placeholder={$tr('asv.ssh.gate.addrPlaceholder')} spellcheck="false" autocomplete="off" aria-label={$tr('asv.ssh.gate.addrOther')} />
              {/if}
            </div>
            <div class="row">
              <button class="btn sm primary" type="button" disabled={gateBusy || !addrValue} onclick={() => void answer(true, { address: addrValue })}>{$tr('asv.ssh.gate.retry')}</button>
              <button class="btn sm" type="button" disabled={gateBusy} onclick={() => void answer(false)}>{$tr('asv.ssh.gate.cancel')}</button>
            </div>
          </div>
        {:else if gate?.kind === 'reinstall'}
          <div class="box warn">
            <p><strong>{$tr('asv.ssh.gate.reinstallTitle')}</strong></p>
            <p>{$tr('asv.ssh.gate.reinstallText')}</p>
            <label class="row check"><input type="checkbox" bind:checked={reinstallOk} /> {$tr('asv.ssh.gate.reinstallCheck')}</label>
            <div class="row">
              <button class="btn sm primary" type="button" disabled={gateBusy || !reinstallOk} onclick={() => void answer(true)}>{$tr('asv.ssh.gate.continue')}</button>
              <button class="btn sm" type="button" disabled={gateBusy} onclick={() => void answer(false)}>{$tr('asv.ssh.gate.cancel')}</button>
            </div>
          </div>
        {/if}

        {#if phase === 'ready'}
          <div class="box good">
            <p><strong>{$tr('asv.ssh.ready')}</strong></p>
            <dl class="kv">
              <dt>{$tr('asv.ssh.readyHost')}</dt>
              <dd>{st.login}@{st.host}:{st.port}</dd>
              {#if st.server}
                <dt>{$tr('asv.ssh.readyAgent')}</dt>
                <dd>{st.server}</dd>
              {/if}
              {#if st.mode}
                <dt>{$tr('asv.ssh.readyRights')}</dt>
                <dd>{$tr(`asv.ssh.mode.${st.mode}`)}</dd>
              {/if}
              <dt>{$tr('asv.ssh.readyApprove')}</dt>
              <dd>{$tr('asv.ssh.readyApproveText')}</dd>
              <dt>{$tr('asv.ssh.readyName')}</dt>
              <dd>{$tr('asv.ssh.readyNameText')}</dd>
            </dl>
            <div class="row">
              <button class="btn primary" type="button" disabled={gateBusy} onclick={() => void install()}>{$tr('asv.ssh.install')}</button>
              <button class="btn" type="button" onclick={edit}>{$tr('asv.ssh.edit')}</button>
            </div>
          </div>
        {:else if phase === 'check_failed'}
          <div class="box crit">
            <p><strong>{$tr('asv.ssh.checkFailed')}</strong></p>
            <p>{failureText(failure.code, auth, { login: st.login, detail: failure.detail }, $tr)}</p>
            <div class="row">
              <button class="btn sm" type="button" onclick={edit}>{$tr('asv.ssh.edit')}</button>
              <button class="btn sm" type="button" onclick={() => void recheck()}>{$tr('asv.ssh.recheck')}</button>
            </div>
          </div>
        {:else if phase === 'install_failed'}
          <div class="box crit">
            <p><strong>{$tr('asv.ssh.installFailed')}</strong></p>
            <p>{failureText(failure.code, auth, { login: st.login, detail: failure.detail, server: st.server ?? '' }, $tr)}</p>
            <div class="row">
              <button class="btn sm" type="button" onclick={() => void recheck()}>{$tr('asv.ssh.retryAll')}</button>
              <button class="btn sm" type="button" onclick={edit}>{$tr('asv.ssh.edit')}</button>
            </div>
          </div>
        {:else if phase === 'done'}
          <div class="box good">
            <p><strong>{$tr('asv.ssh.done', { name: st.new_name ?? st.host })}</strong> {$tr('asv.ssh.doneText')}</p>
            <div class="row">
              {#if newName}
                <button class="btn sm primary" type="button" onclick={() => onopen(newName)}>{$tr('asv.ssh.open')}</button>
              {/if}
              <button class="btn sm" type="button" onclick={again}>{$tr('asv.ssh.again')}</button>
            </div>
          </div>
        {:else if phase === 'closed'}
          <p class="box warn">{$tr('asv.ssh.expired')}</p>
          <div class="row"><button class="btn sm" type="button" onclick={edit}>{$tr('asv.ssh.edit')}</button></div>
        {/if}

        {#if installing}
          <Fold title={$tr('asv.ssh.log')} meta={$tr(`asv.ssh.logLines.${pluralKey(logTotal, $locale)}`, { n: logTotal })} bind:open={logOpen}>
            {#if logTotal > logLines.length}<p class="muted small">{$tr('asv.ssh.logTrimmed', { total: logTotal })}</p>{/if}
            <pre class="log" bind:this={logEl}>{#each logLines as line, i (i)}<span class="ln" class:c={line.startsWith('$ ')}>{line}</span>{/each}</pre>
          </Fold>
        {/if}
      {/if}
    </div>
  {/if}
</section>

<style>
  .fp {
    font-size: 12px;
    color: var(--ink);
  }

  .gatefield {
    margin-top: 7px;
    color: var(--ink);
  }

  .addr {
    display: flex;
    flex-wrap: wrap;
    gap: 7px;
    margin-top: 7px;
    max-width: 378px;
  }

  .addr > :global(*) {
    flex: 1 1 200px;
    min-width: 0;
  }

  .ln {
    display: block;
  }

  .ln.c {
    color: var(--accent);
  }
</style>
