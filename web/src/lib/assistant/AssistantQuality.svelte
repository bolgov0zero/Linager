<script lang="ts">
  import { ApiError, assistantApi, type AssistSettings } from '../../api';
  import { locale, t, tr } from '../../i18n';
  import { COLORS, PRESET_IDS, PRESETS, estimateMbit, presetOf } from './quality';
  import { setAssistSettings } from './settings';

  /** The picture settings of the Assistant of one computer: presets, colours, frame rate, traffic limit, pointer. The "Management" tab and the floating window share it. */
  let { serverId: serverIdProp }: { serverId: string } = $props();
  // Derived: a floating window gets a new `win` object on every move, which must not read the settings again (the form would blink and the edits would be lost).
  const serverId = $derived(serverIdProp);

  let draft = $state<AssistSettings>({ ...PRESETS.normal });
  let loaded = $state(false);
  let loadError = $state('');
  let saving = $state(false);
  let saveError = $state('');
  /** Set after a successful save: the number of open sessions that took the new values; null while there are edits. */
  let applied = $state<number | null>(null);

  const preset = $derived(presetOf(draft));
  const estimate = $derived(estimateMbit(draft));

  const num = (n: number, digits = 1): string => n.toLocaleString($locale === 'ru' ? 'ru-RU' : 'en-GB', { maximumFractionDigits: digits });

  async function loadSettings(): Promise<void> {
    try {
      const got = await assistantApi.settings(serverId);
      draft = { ...got, show_cursor: got.show_cursor === true };
      loadError = '';
      loaded = true;
    } catch (e) {
      loadError = e instanceof ApiError && e.serverMessage ? e.serverMessage : t('common.genericError');
    }
  }

  $effect(() => {
    void serverId;
    loaded = false;
    void loadSettings();
  });

  /** Any edit: the "saved" note goes away. */
  function edit(patch: Partial<AssistSettings>): void {
    draft = { ...draft, ...patch };
    applied = null;
    saveError = '';
  }

  async function save(): Promise<void> {
    saving = true;
    saveError = '';
    try {
      const res = await assistantApi.saveSettings(serverId, draft);
      draft = { ...res.settings, show_cursor: res.settings.show_cursor === true };
      applied = res.applied;
      setAssistSettings(serverId, draft);
    } catch (e) {
      saveError = e instanceof ApiError && e.serverMessage ? e.serverMessage : t('common.genericError');
    } finally {
      saving = false;
    }
  }
</script>

{#if loadError}
  <p class="error" role="alert">{loadError}</p>
  <button class="btn" type="button" onclick={() => void loadSettings()}>{$tr('srv.retry')}</button>
{:else if !loaded}
  <p class="muted">{$tr('common.loading')}</p>
{:else}
  <div class="fld">
    <div class="fl">{$tr('manage.presets')}<small>{$tr('manage.presetsHint')}</small></div>
    <div class="segctl">
      {#each PRESET_IDS as id (id)}
        <button class="chip" type="button" aria-pressed={preset === id} onclick={() => edit({ ...PRESETS[id], show_cursor: draft.show_cursor })}>{$tr(`manage.preset.${id}`)}</button>
      {/each}
      <button class="chip" type="button" aria-pressed={preset === 'custom'} disabled>{$tr('manage.preset.custom')}</button>
    </div>
  </div>
  <div class="fld">
    <div class="fl">{$tr('manage.colors')}<small>{$tr('manage.colorsHint')}</small></div>
    <div class="segctl">
      {#each COLORS as c (c)}
        <button class="chip" type="button" aria-pressed={draft.colors === c} onclick={() => edit({ colors: c })}>{$tr(`manage.color.${c}`)}</button>
      {/each}
    </div>
  </div>
  <div class="fld">
    <div class="fl">{$tr('manage.fps')}<small>{$tr('manage.fpsHint')}</small></div>
    <div class="range">
      <input type="range" min="1" max="30" step="1" value={draft.fps} aria-label={$tr('manage.fps')} oninput={(e) => edit({ fps: Number(e.currentTarget.value) })} />
      <output>{$tr('manage.fpsValue', { n: draft.fps })}</output>
    </div>
  </div>
  <div class="fld">
    <div class="fl">{$tr('manage.limit')}<small>{$tr('manage.limitHint')}</small></div>
    <div>
      <div class="range">
        <input type="range" min="0.5" max="20" step="0.5" value={draft.mbit} disabled={draft.nolimit} aria-label={$tr('manage.limit')} oninput={(e) => edit({ mbit: Number(e.currentTarget.value) })} />
        <output>{draft.nolimit ? $tr('manage.noLimitValue') : $tr('assist.mbitLimit', { v: num(draft.mbit) })}</output>
      </div>
      <label class="check nl">
        <input type="checkbox" checked={draft.nolimit} onchange={(e) => edit({ nolimit: e.currentTarget.checked })} />
        {$tr('manage.noLimit')}
      </label>
    </div>
  </div>
  <div class="fld">
    <div class="fl">{$tr('manage.quality')}<small>{$tr('manage.qualityHint')}</small></div>
    <label class="check">
      <input type="checkbox" checked={draft.auto} onchange={(e) => edit({ auto: e.currentTarget.checked })} />
      {$tr('manage.auto')}
    </label>
  </div>
  <div class="fld">
    <div class="fl">{$tr('manage.cursor')}<small>{$tr('manage.cursorSub')}</small></div>
    <div>
      <label class="check">
        <input type="checkbox" checked={draft.show_cursor} onchange={(e) => edit({ show_cursor: e.currentTarget.checked })} />
        {$tr('manage.cursorShow')}
      </label>
      <p class="small muted note">{$tr('manage.cursorHint')}</p>
    </div>
  </div>
  <p class="est">{$tr('manage.estimate', { peak: num(estimate), idle: num(0.05, 2) })}</p>
  <div class="savebar">
    <button class="btn primary" type="button" disabled={saving} onclick={() => void save()}>{$tr('win.save')}</button>
    <button class="btn" type="button" disabled={saving} onclick={() => edit(PRESETS.normal)}>{$tr('manage.reset')}</button>
    {#if applied !== null}<span class="saved" role="status">{$tr('manage.saved', { n: applied })}</span>{/if}
    {#if saveError}<span class="error" role="alert">{saveError}</span>{/if}
  </div>
{/if}

<style>
  .fld {
    display: grid;
    grid-template-columns: minmax(110px, 150px) minmax(0, 1fr);
    gap: 10px;
    align-items: center;
    padding: 10px 0;
    border-bottom: 1px dashed var(--seg-off);
  }

  .fl {
    font-family: var(--font-display);
  }

  .fl small {
    display: block;
    font-family: var(--font-body);
    color: var(--muted);
    font-size: 11px;
  }

  .segctl {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }

  .chip:disabled {
    cursor: default;
    opacity: 0.55;
  }

  .range {
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .range input[type='range'] {
    flex: 1;
    min-width: 120px;
    height: 20px;
    accent-color: var(--accent);
  }

  .range output {
    min-width: 92px;
    text-align: right;
    font-family: var(--font-display);
    font-size: 14px;
  }

  .nl {
    margin-top: 8px;
  }

  .note {
    margin: 10px 0 0;
  }

  .est {
    margin: 12px 0 0;
    padding: 8px 10px;
    border: 2px dashed var(--line);
    color: var(--muted);
    font-size: 12px;
  }

  .savebar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
    margin-top: 14px;
  }

  .saved {
    color: var(--good);
    font-size: 12px;
  }

  p {
    margin-block: 0 8px;
  }

  @media (max-width: 860px) {
    .fld {
      grid-template-columns: 1fr;
    }
  }
</style>
