<script lang="ts">
  import PixIcon from '../../ui/PixIcon.svelte';
  import { untrack } from 'svelte';
  import { api, ApiError, type F2bSettings } from '../../../api';
  import { t, tr as T } from '../../../i18n';
  import { showToast } from '../../toast';
  import Modal from '../../Modal.svelte';
  import Select from '../../ui/Select.svelte';
  import { isIPv4, isIPv6 } from './fw-sets';
  import { durationText, joinBan, matchPreset, PRESETS, previewConf, splitBan, type Preset, type Unit } from './f2b-util';

  let {
    serverId,
    settings,
    selfIp,
    onsaved,
    onclose,
  }: {
    serverId: string;
    settings: F2bSettings;
    selfIp: string;
    onsaved: () => Promise<void>;
    onclose: () => void;
  } = $props();

  const tx = (key: string, vars?: Record<string, string | number>): string => t(key, undefined, vars);
  const LOOPBACK = ['127.0.0.0/8', '127.0.0.1/8', '127.0.0.1', '::1'];

  // the form starts from the current settings and is edited locally
  const init = untrack(() => settings);
  const start = splitBan(init.bantime);
  let maxretry = $state(init.maxretry);
  let findMin = $state(Math.max(1, Math.round(init.findtime / 60)));
  let banValue = $state(start.value);
  let banUnit = $state<Unit>(start.unit);
  let increment = $state(init.increment);
  let recidive = $state(init.recidive);
  let ignore = $state<string[]>(init.ignore.filter((x) => !LOOPBACK.includes(x) && x !== selfIp));
  let newIgnore = $state('');
  let ignoreError = $state<string | null>(null);
  let busy = $state(false);
  let error = $state<string | null>(null);

  const bantime = $derived(joinBan(banValue, banUnit));
  const draft = $derived<F2bSettings>({ maxretry, findtime: findMin * 60, bantime, increment, recidive, ignore });
  const active = $derived(matchPreset(draft));
  const valid = $derived(
    Number.isInteger(maxretry) && maxretry >= 1 && maxretry <= 100 &&
      Number.isInteger(findMin) && findMin >= 1 && findMin <= 1440 &&
      (banUnit === 'perm' || (Number.isInteger(banValue) && banValue >= 1)),
  );
  const summary = $derived(tx('sec.f2b.summaryLong', { n: maxretry, min: findMin, ban: durationText(bantime, tx) }));
  const units = $derived([
    { value: 'm' as Unit, label: $T('sec.f2b.unit.m') },
    { value: 'h' as Unit, label: $T('sec.f2b.unit.h') },
    { value: 'd' as Unit, label: $T('sec.f2b.unit.d') },
    { value: 'perm' as Unit, label: $T('sec.f2b.unit.perm') },
  ]);

  function applyPreset(p: Preset): void {
    const v = PRESETS[p];
    maxretry = v.maxretry;
    findMin = v.findtime / 60;
    const b = splitBan(v.bantime);
    banValue = b.value;
    banUnit = b.unit;
  }

  function addIgnore(): void {
    const v = newIgnore.trim();
    if (!v) return;
    const [addr, prefix] = v.split('/');
    const ok = (isIPv4(addr) || isIPv6(addr)) && (prefix === undefined || (/^\d{1,3}$/.test(prefix) && Number(prefix) <= (addr.includes(':') ? 128 : 32)));
    if (!ok) {
      ignoreError = $T('sec.f2b.ignore.bad');
      return;
    }
    ignoreError = null;
    if (!ignore.includes(v) && v !== selfIp) ignore = [...ignore, v];
    newIgnore = '';
  }

  async function save(): Promise<void> {
    if (!valid || busy) return;
    busy = true;
    error = null;
    try {
      await api.securityF2bSettings(serverId, draft);
      showToast(tx('sec.f2b.saved'));
      await onsaved();
      onclose();
    } catch (e) {
      error = e instanceof ApiError && e.serverMessage ? e.serverMessage : tx('common.genericError');
    } finally {
      busy = false;
    }
  }
</script>

<Modal title={$T('sec.f2b.settingsTitle')} labelId="f2b-settings-title" {onclose}>
  <div class="stack">
    <h4>{$T('sec.f2b.threshold')}</h4>
    <div class="row" role="group" aria-label={$T('sec.f2b.how')}>
      {#each ['soft', 'norm', 'hard'] as p (p)}
        <button class="chip" type="button" aria-pressed={active === p} onclick={() => applyPreset(p as Preset)}>{$T(`sec.f2b.preset.${p}`)}</button>
      {/each}
      <span class="small muted">{$T('sec.f2b.orManual')}</span>
    </div>
    <div class="form">
      <label class="field">
        <span class="label">{$T('sec.f2b.maxretry')}</span>
        <input class="input" type="number" min="1" max="100" bind:value={maxretry} />
      </label>
      <label class="field">
        <span class="label">{$T('sec.f2b.findtime')}</span>
        <input class="input" type="number" min="1" max="1440" bind:value={findMin} />
      </label>
      <div class="field">
        <span class="label">{$T('sec.f2b.bantime')}</span>
        <span class="row nowrap">
          {#if banUnit !== 'perm'}
            <input class="input num" type="number" min="1" aria-label={$T('sec.f2b.bantime')} bind:value={banValue} />
          {/if}
          <Select size="sm" bind:value={banUnit} ariaLabel={$T('sec.f2b.bantime')} options={units} />
        </span>
      </div>
    </div>
    <p class="small muted">{summary}</p>
  </div>

  <div class="stack">
    <h4>{$T('sec.f2b.repeat')}</h4>
    <label class="check"><input type="checkbox" bind:checked={increment} /> <span>{$T('sec.f2b.increment')}</span></label>
    <label class="check"><input type="checkbox" bind:checked={recidive} /> <span>{$T('sec.f2b.recidive')}</span></label>
  </div>

  <div class="stack">
    <h4>{$T('sec.f2b.ignore')}</h4>
    <div class="ips">
      <span class="ipchip mono">127.0.0.0/8</span>
      {#if selfIp}<span class="ipchip mono">{selfIp} · {$T('sec.f2b.yourAddress')}</span>{/if}
      {#each ignore as ip (ip)}
        <span class="ipchip mono">{ip}<button type="button" aria-label={$T('sec.f2b.ignore.remove', { ip })} onclick={() => (ignore = ignore.filter((x) => x !== ip))}><PixIcon name="x" size={12} /></button></span>
      {/each}
    </div>
    <form
      class="row nowrap"
      onsubmit={(e) => {
        e.preventDefault();
        addIgnore();
      }}
    >
      <input
        class="input mono grow"
        type="text"
        autocomplete="off"
        spellcheck="false"
        placeholder={$T('sec.f2b.ignore.placeholder')}
        aria-label={$T('sec.f2b.ignore')}
        bind:value={newIgnore}
        oninput={() => (ignoreError = null)}
      />
      <button class="btn" type="submit" disabled={!newIgnore.trim()}>{$T('sec.f2b.ignore.add')}</button>
    </form>
    {#if ignoreError}<p class="error small" role="alert">{ignoreError}</p>{/if}
    <p class="small muted">{$T('sec.f2b.ignore.note')}</p>
  </div>

  <details class="stack">
    <summary class="small muted">{$T('sec.f2b.showConf')}</summary>
    <pre class="cmd mono">{previewConf(draft, selfIp)}</pre>
  </details>

  {#if error}<p class="error" role="alert">{error}</p>{/if}
  <p class="small muted">{$T('sec.f2b.saveNote')}</p>
  <div class="row end">
    <button class="btn" type="button" disabled={busy} onclick={onclose}>{$T('common.cancel')}</button>
    <button class="btn primary" type="button" disabled={busy || !valid} onclick={() => void save()}>{$T('sec.f2b.save')}</button>
  </div>
</Modal>

<style>
  h4 {
    margin: 0;
    font-family: var(--font-display);
    font-size: 13px;
    font-weight: 400;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: var(--muted);
  }

  p {
    margin: 0;
  }

  .form {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: 11px;
    align-items: end;
  }

  .nowrap {
    flex-wrap: nowrap;
  }

  .input {
    height: 32px;
    padding-block: 0;
  }

  .input.num {
    width: 72px;
  }

  .grow {
    flex: 1;
    min-width: 0;
  }

  .form :global(.sel),
  .nowrap :global(.sel) {
    height: 32px;
    padding-block: 0;
  }

  .row :global(.chip),
  .row :global(.btn) {
    display: inline-flex;
    align-items: center;
    height: 32px;
    padding-block: 0;
  }

  .ips {
    display: flex;
    flex-wrap: wrap;
    gap: 5px;
  }

  .ipchip {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    padding: 2px 7px;
    border: 2px solid var(--line);
    font-size: 12px;
  }

  .ipchip button {
    padding: 0 2px;
    border: 0;
    background: none;
    color: var(--muted);
    font: inherit;
    cursor: pointer;
  }

  .ipchip button:hover {
    color: var(--crit);
  }

  .cmd {
    margin: 0;
    padding: 9px;
    border: 2px solid var(--line);
    background: var(--bg);
    font-size: 11px;
    white-space: pre-wrap;
  }

  summary {
    cursor: pointer;
  }

  .error {
    color: var(--crit);
  }

  @media (max-width: 640px) {
    .form {
      grid-template-columns: minmax(0, 1fr);
    }
  }

  @media (max-width: 720px) {
    .ipchip {
      min-height: 44px;
      padding: 0 0 0 9px;
      overflow-wrap: anywhere;
    }

    .ipchip button {
      min-width: 44px;
      min-height: 44px;
      padding: 0;
      font-size: 18px;
    }

    .input.num {
      width: 96px;
    }

    summary {
      min-height: 44px;
      display: flex;
      align-items: center;
    }
  }
</style>
