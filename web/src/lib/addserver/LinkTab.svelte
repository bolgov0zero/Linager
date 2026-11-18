<script lang="ts">
  import { onMount } from 'svelte';
  import { api, ApiError, isUnauthorized, type Group, type JoinCommand, type JoinLink, type JoinLinkCreated } from '../../api';
  import { locale, t, tr as trans } from '../../i18n';
  import Fold from '../ui/Fold.svelte';
  import Select from '../ui/Select.svelte';
  import { isPhone } from '../ui/viewport';
  import { fmtDateTime } from '../updates/format';
  import { showToast } from '../toast';
  import { fmtIn, joinScriptText } from './link';

  let {
    active,
    groups,
    onauthlost,
  }: {
    active: boolean;
    groups: Group[];
    onauthlost: () => void;
  } = $props();

  const POLL_MS = 5000;
  const TTLS = [900, 3600, 86400, 604800];
  const USES = [1, 5, 10, 25, 0];

  let ttl = $state(3600);
  let uses = $state(1);
  let groupId = $state(0);
  let nets = $state('');
  let auto = $state(false);
  let busy = $state(false);
  let formError = $state('');

  /** The link just made: the token and the commands live only here, in memory, and are never stored anywhere. */
  let created = $state<JoinLinkCreated | null>(null);
  let createdWith = $state({ ttl: 3600, uses: 1, auto: false });
  let showScript = $state(false);
  let fingerprint = $state('');

  let links = $state<JoinLink[]>([]);
  let linksOpen = $state(false);
  let revoking = $state<Record<number, boolean>>({});
  let now = $state(Date.now());

  const ttlOptions = $derived(TTLS.map((v) => ({ value: v, label: $trans(`asv.link.ttl.${v}`) })));
  const usesOptions = $derived(USES.map((v) => ({ value: v, label: v === 0 ? $trans('asv.link.uses.0') : String(v) })));
  const groupOptions = $derived([
    { value: 0, label: $trans('asv.group.none') },
    ...groups.map((g) => ({ value: g.id, label: g.name })),
  ]);
  /** The system of the new computer: the install command differs. */
  let platform = $state<'linux' | 'windows'>('linux');
  const commands = $derived(created?.commands.filter((c) => (c.kind === 'external' || c.kind === 'local') && (c.platform ?? 'linux') === platform) ?? []);
  const scriptText = $derived(created && platform === 'linux' && commands.length > 0 ? joinScriptText(created.token, commands[0].command, fingerprint) : '');

  function parseNets(text: string): string[] | null {
    const list = text.split(',').map((s) => s.trim()).filter(Boolean);
    return list.every((n) => /^[0-9a-f:.]+(\/\d{1,3})?$/i.test(n)) ? list : null;
  }

  async function loadLinks(): Promise<void> {
    try {
      links = await api.joinLinks();
    } catch (e) {
      if (isUnauthorized(e)) onauthlost();
    }
  }

  async function create(): Promise<void> {
    if (busy) return;
    formError = '';
    const list = parseNets(nets);
    if (list === null) {
      formError = t('asv.link.netsBad');
      return;
    }
    busy = true;
    try {
      const res = await api.joinLinkCreate({
        ttl_seconds: ttl,
        max_uses: uses,
        auto_approve: auto,
        networks: list,
        group_id: groupId === 0 ? null : groupId,
      });
      created = res;
      createdWith = { ttl, uses, auto };
      showScript = false;
      showToast(t('asv.link.createdToast'));
      if (!fingerprint) {
        try {
          fingerprint = (await api.connectInfo()).fingerprint;
        } catch {
          // the script is shown without the fingerprint line
        }
      }
      await loadLinks();
    } catch (e) {
      if (isUnauthorized(e)) {
        onauthlost();
        return;
      }
      if (e instanceof ApiError && e.status === 422) formError = e.serverMessage ?? t('common.genericError');
      else showToast(t('common.genericError'));
    } finally {
      busy = false;
    }
  }

  async function copy(cmd: JoinCommand): Promise<void> {
    try {
      await navigator.clipboard.writeText(cmd.command);
      showToast(t('common.copied'));
    } catch {
      showToast(t('common.copyFailed'));
    }
  }

  async function revoke(link: JoinLink): Promise<void> {
    if (revoking[link.id]) return;
    revoking[link.id] = true;
    try {
      await api.joinLinkRevoke(link.id);
      if (created?.id === link.id) created = null;
      showToast(t('asv.link.revoked'));
      await loadLinks();
    } catch (e) {
      if (isUnauthorized(e)) {
        onauthlost();
        return;
      }
      showToast(t('common.genericError'));
    } finally {
      revoking[link.id] = false;
    }
  }

  function expiresText(link: JoinLink): string {
    const left = link.expires_at - Math.floor(now / 1000);
    return left <= 0 ? $trans('asv.link.expired') : $trans('asv.link.in', { t: fmtIn(left, $trans) });
  }

  function usedText(link: JoinLink): string {
    return $trans('asv.link.usedOf', { n: link.uses, max: link.max_uses === 0 ? '∞' : link.max_uses });
  }

  onMount(() => {
    void loadLinks();
    const timer = setInterval(() => {
      now = Date.now();
      if (active && !document.hidden) void loadLinks();
    }, POLL_MS);
    return () => clearInterval(timer);
  });
</script>

<section class="panel">
  <h3 class="page">{$trans('asv.tab.link')}</h3>
  <p class="lead">{$trans('asv.link.lead')}</p>

  <div class="sec">
    <h4>{$trans('asv.link.new')}</h4>
    <div class="card">
      <div class="grid">
        <label class="field">
          <span class="label">{$trans('asv.link.ttl')}</span>
          <Select bind:value={ttl} options={ttlOptions} />
        </label>
        <label class="field">
          <span class="label">{$trans('asv.link.uses')}</span>
          <Select bind:value={uses} options={usesOptions} />
        </label>
        <label class="field">
          <span class="label">{$trans('asv.link.group')}</span>
          <Select bind:value={groupId} options={groupOptions} />
        </label>
      </div>
      <label class="field">
        <span class="label">{$trans('asv.link.nets')}</span>
        <input
          class="input mono"
          class:bad={!!formError && formError === $trans('asv.link.netsBad')}
          bind:value={nets}
          placeholder={$trans('asv.link.netsPlaceholder')}
          spellcheck="false"
          autocomplete="off"
          oninput={() => (formError = '')}
        />
        <span class="label">{$trans('asv.link.netsHint')}</span>
      </label>
      <label class="switch">
        <input type="checkbox" role="switch" bind:checked={auto} />
        <span>{$trans('asv.link.auto')}<small>{$trans('asv.link.autoHint')}</small></span>
      </label>
      {#if formError}<p class="error small" role="alert">{formError}</p>{/if}
      <div class="row end">
        <button class="btn primary" type="button" disabled={busy} onclick={() => void create()}>{$trans('asv.link.create')}</button>
      </div>
    </div>
  </div>

  {#if created}
    <div class="sec">
      <h4>{$trans('asv.link.created')}</h4>
      <div class="card link">
        <p class="small">
          <strong>{$trans('asv.link.once')}</strong>
          {$trans('asv.link.onceRest', {
            ttl: $trans(`asv.link.ttl.${createdWith.ttl}`),
            max: createdWith.uses === 0 ? $trans('asv.link.uses.0') : createdWith.uses,
            mode: createdWith.auto ? $trans('asv.link.modeAuto') : $trans('asv.link.modeManual'),
          })}
        </p>
        <div class="seg" role="tablist" aria-label={$trans('asv.link.platform')}>
          <button class="btn sm" class:ink={platform === 'linux'} type="button" role="tab" aria-selected={platform === 'linux'} onclick={() => (platform = 'linux')}>Linux</button>
          <button class="btn sm" class:ink={platform === 'windows'} type="button" role="tab" aria-selected={platform === 'windows'} onclick={() => (platform = 'windows')}>Windows</button>
        </div>
        {#if platform === 'windows'}<p class="muted small">{$trans('asv.link.winHint')}</p>{/if}
        {#each commands as cmd (cmd.kind)}
          <div class="cmdblock">
            <div class="row between">
              <strong>{$trans(cmd.kind === 'external' ? 'asv.link.ext' : 'asv.link.loc')}</strong>
              <button class="btn sm" type="button" onclick={() => void copy(cmd)}>{$trans('common.copy')}</button>
            </div>
            <p class="muted small">{$trans(cmd.kind === 'external' ? 'asv.link.extHint' : 'asv.link.locHint')}</p>
            <pre class="cmd">{cmd.command}</pre>
          </div>
        {/each}
        {#if platform === 'linux'}
          <div class="row between">
            <button class="btn sm" type="button" onclick={() => (showScript = !showScript)}>
              {$trans(showScript ? 'asv.link.hideScript' : 'asv.link.showScript')}
            </button>
            <span class="muted small">{$trans('asv.link.scriptHint')}</span>
          </div>
          {#if showScript}<pre class="cmd code">{scriptText}</pre>{/if}
        {/if}
        <p class="box info">
          <strong>{$trans('asv.link.whyTitle')}</strong>
          {$trans('asv.link.why')}
        </p>
      </div>
    </div>
  {/if}

  <Fold title={$trans('asv.link.list')} meta={String(links.length)} bind:open={linksOpen}>
    {#if links.length === 0}
      <p class="muted small">{$trans('asv.link.listEmpty')}</p>
    {:else if $isPhone}
      <div class="cards">
        {#each links as link (link.id)}
          <div class="lcard">
            <div class="row between nowrap">
              <strong class="mono">{link.label}</strong>
              <button class="btn sm" type="button" disabled={revoking[link.id]} onclick={() => void revoke(link)}>{$trans('asv.link.revoke')}</button>
            </div>
            <dl class="kv">
              <dt>{$trans('asv.link.colCreated')}</dt>
              <dd>{fmtDateTime(link.created_at, $trans, $locale)}</dd>
              <dt>{$trans('asv.link.colExpires')}</dt>
              <dd>{expiresText(link)}</dd>
              <dt>{$trans('asv.link.colUsed')}</dt>
              <dd>
                {usedText(link)}
                {#each link.used as u (u.at + u.host)}
                  <div class="muted small">{u.host} · {fmtDateTime(u.at, $trans, $locale)}</div>
                {/each}
              </dd>
              <dt>{$trans('asv.link.colMode')}</dt>
              <dd>{$trans(link.auto_approve ? 'asv.link.modeAutoShort' : 'asv.link.modeManualShort')}</dd>
              <dt>{$trans('asv.link.colNets')}</dt>
              <dd class="mono">{link.networks.length > 0 ? link.networks.join(', ') : $trans('asv.link.netsAny')}</dd>
              <dt>{$trans('asv.link.colGroup')}</dt>
              <dd>{link.group_name ?? '—'}</dd>
            </dl>
          </div>
        {/each}
      </div>
    {:else}
      <div class="wrap">
        <table class="tbl">
          <thead>
            <tr>
              <th>{$trans('asv.link.colLink')}</th>
              <th>{$trans('asv.link.colCreated')}</th>
              <th>{$trans('asv.link.colExpires')}</th>
              <th>{$trans('asv.link.colUsed')}</th>
              <th>{$trans('asv.link.colMode')}</th>
              <th>{$trans('asv.link.colNets')}</th>
              <th>{$trans('asv.link.colGroup')}</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {#each links as link (link.id)}
              <tr>
                <td class="mono">{link.label}</td>
                <td>{fmtDateTime(link.created_at, $trans, $locale)}</td>
                <td>{expiresText(link)}</td>
                <td>
                  {usedText(link)}
                  {#each link.used as u (u.at + u.host)}
                    <div class="muted small">{u.host} · {fmtDateTime(u.at, $trans, $locale)}</div>
                  {/each}
                </td>
                <td>{$trans(link.auto_approve ? 'asv.link.modeAutoShort' : 'asv.link.modeManualShort')}</td>
                <td class="mono wrapok">{link.networks.length > 0 ? link.networks.join(', ') : $trans('asv.link.netsAny')}</td>
                <td>{link.group_name ?? '—'}</td>
                <td><button class="btn sm" type="button" disabled={revoking[link.id]} onclick={() => void revoke(link)}>{$trans('asv.link.revoke')}</button></td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {/if}
  </Fold>
</section>

<style>
  .cmdblock {
    display: flex;
    flex-direction: column;
    gap: 5px;
  }

  .cards {
    display: flex;
    flex-direction: column;
    gap: 9px;
  }

  .lcard {
    display: flex;
    flex-direction: column;
    gap: 7px;
    padding-top: 9px;
    border-top: 2px dashed var(--seg-off);
  }

  .lcard:first-child {
    padding-top: 0;
    border-top: 0;
  }

  .nowrap {
    flex-wrap: nowrap;
  }
  .seg {
    display: flex;
    gap: 5px;
  }
</style>
