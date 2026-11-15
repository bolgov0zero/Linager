<script lang="ts">
  import { api, ApiError, type SecState } from '../../../api';
  import { locale, t, tr as T } from '../../../i18n';
  import { showToast } from '../../toast';
  import Select from '../../ui/Select.svelte';
  import F2bBansModal from './F2bBansModal.svelte';
  import F2bSettingsModal from './F2bSettingsModal.svelte';
  import StreamModal from './StreamModal.svelte';
  import { durationText, PRESETS, type Preset } from './f2b-util';

  let {
    serverId,
    sec,
    unreachable,
    onchanged,
  }: {
    serverId: string;
    sec: SecState;
    unreachable: boolean;
    /** Reload the state of the tab after a change. */
    onchanged: () => Promise<void>;
  } = $props();

  const f2b = $derived(sec.fail2ban);
  const settings = $derived(f2b.settings);
  const bannedNow = $derived(f2b.bans.length);
  const totalBanned = $derived(f2b.jails.reduce((n, j) => n + j.total_banned, 0));
  const failedNow = $derived(f2b.jails.reduce((n, j) => n + j.currently_failed, 0));

  const tx = (key: string, vars?: Record<string, string | number>): string => t(key, undefined, vars);

  // ---- install ----
  let preset = $state<Preset>('norm');
  let confirmInstall = $state(false);
  let installing = $state(false);
  const PRESET_KEYS: Preset[] = ['soft', 'norm', 'hard'];

  function presetLine(p: Preset): string {
    const v = PRESETS[p];
    return tx('sec.f2b.presetLine', { n: v.maxretry, min: v.findtime / 60, ban: durationText(v.bantime, tx) });
  }

  const installCommands = $derived.by(() => {
    const v = PRESETS[preset];
    const ign = ['127.0.0.1/8', '::1', sec.self_ip].filter(Boolean).join(' ');
    return [
      'apt-get install -y fail2ban python3-systemd',
      '# /etc/fail2ban/jail.d/linager.conf  [sshd]  maxretry=' + v.maxretry + '  findtime=' + v.findtime + 's  bantime=' + v.bantime + 's  ignoreip=' + ign,
      'systemctl enable fail2ban && systemctl restart fail2ban',
    ].join('\n');
  });

  // ---- manual ban ----
  let banIp = $state('');
  let banDuration = $state(0);
  let banBusy = $state(false);
  let banError = $state<string | null>(null);

  const durations = $derived([
    { value: 0, label: $T('sec.f2b.manual.default') },
    { value: 3600, label: $T('sec.f2b.manual.h1') },
    { value: 86400, label: $T('sec.f2b.manual.h24') },
    { value: 604800, label: $T('sec.f2b.manual.d7') },
  ]);

  function errorOf(e: unknown): string {
    if (e instanceof ApiError) {
      if (e.status === 0) return tx('common.networkError');
      if (e.serverMessage) return e.serverMessage;
    }
    return tx('common.genericError');
  }

  async function ban(ip: string, duration: number): Promise<string | null> {
    try {
      await api.securityBan(serverId, ip, duration);
      showToast(tx('sec.f2b.banned.done', { ip }));
      await onchanged();
      return null;
    } catch (e) {
      return errorOf(e);
    }
  }

  async function submitBan(): Promise<void> {
    const ip = banIp.trim();
    if (!ip || banBusy) return;
    banBusy = true;
    banError = await ban(ip, banDuration);
    if (!banError) banIp = '';
    banBusy = false;
  }

  // ---- modals ----
  let settingsOpen = $state(false);
  let bansOpen = $state(false);

  const summary = $derived(
    settings
      ? tx('sec.f2b.summary', { n: settings.maxretry, min: Math.round(settings.findtime / 60), ban: durationText(settings.bantime, tx) })
      : '',
  );
</script>

<section class="card stack" aria-labelledby="sec-f2b">
  <div class="f2h">
    <h3 id="sec-f2b">
      fail2ban
      {#if f2b.installed && f2b.active}
        <span class="badge ok">{$T('sec.f2b.running')}</span>
        {#if summary}<span class="muted small sum">sshd · {summary}</span>{/if}
      {:else if f2b.installed}
        <span class="badge warn">{$T('sec.f2b.stoppedBadge')}</span>
      {:else}
        <span class="badge warn">{$T('sec.f2b.notInstalled')}</span>
      {/if}
    </h3>
    {#if f2b.installed && f2b.active}
      <div class="row">
        <button class="btn" type="button" disabled={unreachable} onclick={() => (bansOpen = true)}>
          {$T('sec.f2b.list')}
          {#if bannedNow > 0}<span class="badge crit cnt">{bannedNow}</span>{/if}
        </button>
        <button class="btn" type="button" disabled={unreachable || !settings} onclick={() => (settingsOpen = true)}>{$T('sec.f2b.settings')}</button>
      </div>
    {/if}
  </div>

  {#if !f2b.installed}
    <div class="callout">
      <div>
        <span class="num crit">{sec.failures.total.toLocaleString($locale)}</span>
        <p class="small muted">{$T('sec.f2b.failuresWeek')}</p>
      </div>
      <div>
        <span class="num">{sec.failures.addresses.toLocaleString($locale)}</span>
        <p class="small muted">{$T('sec.f2b.addresses')}</p>
      </div>
      <p class="why">{$T('sec.f2b.why')}</p>
    </div>
    <div class="stack">
      <h4>{$T('sec.f2b.how')}</h4>
      <div class="presets" role="group" aria-label={$T('sec.f2b.how')}>
        {#each PRESET_KEYS as p (p)}
          <button class="preset" type="button" aria-pressed={preset === p} onclick={() => (preset = p)}>
            <b>{$T(`sec.f2b.preset.${p}`)}</b>
            <span class="small muted">{presetLine(p)}</span>
            {#if p === 'norm'}<span class="badge ok tag">{$T('sec.f2b.recommended')}</span>{/if}
          </button>
        {/each}
      </div>
      <p class="small muted">
        {#if sec.self_ip}{$T('sec.f2b.selfNote', { ip: sec.self_ip })}{/if}
        {$T('sec.f2b.laterNote')}
      </p>
    </div>
    {#if !confirmInstall}
      <div class="row end">
        <button class="btn primary" type="button" disabled={unreachable} onclick={() => (confirmInstall = true)}>{$T('sec.f2b.install')}</button>
      </div>
    {:else}
      <div class="confirm stack" role="alertdialog" aria-label={$T('sec.f2b.install')}>
        <p>{$T('sec.f2b.installText', { preset: $T(`sec.f2b.preset.${preset}`) })}</p>
        <pre class="cmd mono">{installCommands}</pre>
        <p class="small muted">{$T('sec.f2b.installNote')}</p>
        <div class="row end">
          <button class="btn" type="button" onclick={() => (confirmInstall = false)}>{$T('common.cancel')}</button>
          <button
            class="btn primary"
            type="button"
            onclick={() => {
              confirmInstall = false;
              installing = true;
            }}>{$T('sec.f2b.installGo')}</button
          >
        </div>
      </div>
    {/if}
  {:else if !f2b.active}
    <p class="muted">{$T('sec.f2b.stopped')}</p>
  {:else}
    <div class="stats">
      <button class="stat link" type="button" onclick={() => (bansOpen = true)}>
        <b class:crit={bannedNow > 0}>{bannedNow}</b><span>{$T('sec.f2b.statNow')}</span>
      </button>
      <div class="stat"><b>{totalBanned.toLocaleString($locale)}</b><span>{$T('sec.f2b.statTotal')}</span></div>
      <div class="stat"><b>{failedNow}</b><span>{$T('sec.f2b.statWindow')}</span></div>
    </div>
    {#if !f2b.jails.some((j) => j.name === 'sshd')}
      <p class="notice" role="status">{$T('sec.f2b.noSshd')}</p>
    {/if}
    <form
      class="manual"
      onsubmit={(e) => {
        e.preventDefault();
        void submitBan();
      }}
    >
      <b class="small">{$T('sec.f2b.manual')}</b>
      <input
        class="input mono"
        type="text"
        autocomplete="off"
        spellcheck="false"
        placeholder="198.51.100.7"
        aria-label={$T('sec.f2b.manual')}
        size="16"
        bind:value={banIp}
        oninput={() => (banError = null)}
      />
      <Select size="sm" bind:value={banDuration} ariaLabel={$T('sec.f2b.manual.duration')} options={durations} />
      <button class="btn danger" type="submit" disabled={banBusy || unreachable || !banIp.trim()}>{$T('sec.f2b.manual.go')}</button>
      <span class="small muted">{$T('sec.f2b.manual.self')}</span>
    </form>
    {#if banError}<p class="error small" role="alert">{banError}</p>{/if}
  {/if}
</section>

{#if installing}
  <StreamModal
    title={$T('sec.f2b.installTitle')}
    {serverId}
    kind="f2b-install"
    params={{ preset }}
    onend={(failed) => {
      if (!failed) showToast(tx('sec.f2b.installed'));
      void onchanged();
    }}
    onclose={() => (installing = false)}
  />
{/if}

{#if settingsOpen && settings}
  <F2bSettingsModal {serverId} {settings} selfIp={sec.self_ip ?? ''} onsaved={onchanged} onclose={() => (settingsOpen = false)} />
{/if}

{#if bansOpen}
  <F2bBansModal {serverId} bans={f2b.bans} onchanged={onchanged} onclose={() => (bansOpen = false)} />
{/if}

<style>
  h3,
  h4,
  p {
    margin: 0;
  }

  h3 {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 10px;
    font-size: 16px;
    color: var(--muted);
    text-transform: uppercase;
    letter-spacing: 0.06em;
  }

  h4 {
    font-family: var(--font-display);
    font-size: 14px;
    font-weight: 400;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: var(--muted);
  }

  .sum {
    text-transform: none;
    letter-spacing: 0;
  }

  .f2h {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
  }

  .f2h :global(.btn) {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    height: 36px;
    padding-block: 0;
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

  .badge.ok {
    color: var(--good);
  }

  .badge.warn {
    color: var(--warn);
  }

  .badge.crit {
    color: var(--crit);
  }

  .cnt {
    line-height: 1.1;
  }

  .callout {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 16px 28px;
    padding: 12px;
    border: 2px dashed var(--line);
  }

  .callout .num {
    font-family: var(--font-display);
    font-size: 30px;
    line-height: 1;
  }

  .num.crit,
  .crit {
    color: var(--crit);
  }

  .why {
    flex: 1;
    min-width: 240px;
  }

  .presets {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: 10px;
  }

  .preset {
    position: relative;
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 4px;
    padding: 10px 12px;
    border: 2px solid var(--line);
    background: var(--surface);
    color: inherit;
    font: inherit;
    text-align: left;
    cursor: pointer;
  }

  .preset[aria-pressed='true'] {
    border-color: var(--accent);
    box-shadow: 3px 3px 0 var(--accent);
  }

  .preset .tag {
    margin-top: 2px;
    color: var(--good);
  }

  .confirm {
    padding: 12px;
    border: 2px solid var(--accent);
    background: color-mix(in srgb, var(--accent) 7%, transparent);
  }

  .cmd {
    margin: 0;
    padding: 10px;
    border: 2px solid var(--line);
    background: var(--bg);
    font-size: 12px;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }

  .stats {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: 10px;
  }

  .stat {
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: 10px 12px;
    border: 2px solid var(--line);
    background: none;
    color: inherit;
    font: inherit;
    text-align: left;
  }

  .stat b {
    font-family: var(--font-display);
    font-size: 26px;
    font-weight: 400;
    line-height: 1.1;
  }

  .stat span {
    color: var(--muted);
    font-size: 13px;
  }

  .stat.link {
    cursor: pointer;
  }

  .stat.link:hover,
  .stat.link:focus-visible {
    border-color: var(--accent);
  }

  .manual {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px 10px;
  }

  .manual .input {
    width: 190px;
    height: 36px;
    padding-block: 0;
  }

  .manual :global(.sel) {
    height: 36px;
    padding-block: 0;
  }

  .manual :global(.btn) {
    display: inline-flex;
    align-items: center;
    height: 36px;
    padding-block: 0;
  }

  .notice {
    padding: 8px 12px;
    border: 2px solid var(--warn);
    color: var(--warn);
  }

  .error {
    color: var(--crit);
  }

  @media (max-width: 700px) {
    .presets,
    .stats {
      grid-template-columns: minmax(0, 1fr);
    }
  }
</style>
