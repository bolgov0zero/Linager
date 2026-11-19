<script lang="ts">
  import { assistantApi, type AssistSettings } from '../../api';
  import { locale, tr } from '../../i18n';
  import type { AssistInfo, AssistPhase } from './AssistantSession.svelte';

  /** The line under the screen: what really happens (resolution, colours, frames, traffic) and the way to the settings. */
  let {
    serverId,
    phase,
    info,
    onsettings,
  }: {
    serverId: string;
    phase: AssistPhase;
    info: AssistInfo | null;
    onsettings: () => void;
  } = $props();

  let settings = $state<AssistSettings | null>(null);

  async function load(): Promise<void> {
    try {
      settings = await assistantApi.settings(serverId);
    } catch {
      // the limits are just not shown
    }
  }

  // The settings are changed on the Manage tab or in another window while this one is open, so they are read again.
  $effect(() => {
    if (phase !== 'open') return;
    void serverId;
    void load();
    const timer = setInterval(() => void load(), 20000);
    return () => clearInterval(timer);
  });

  const live = $derived(phase === 'open' && info !== null);
  const num = (n: number, digits = 1): string => n.toLocaleString($locale === 'ru' ? 'ru-RU' : 'en-GB', { maximumFractionDigits: digits });
  const limit = $derived(settings ? (settings.nolimit ? $tr('assist.noLimit') : $tr('assist.mbitLimit', { v: num(settings.mbit) })) : '');
</script>

<div class="stat">
  <span>{live && info ? `${info.w}×${info.h}` : '—'}</span>
  {#if settings}<span>{$tr('assist.stat.colors')} <b>{$tr(`assist.colorNote.${settings.colors}`)}</b></span>{/if}
  <span>{$tr('assist.stat.frames')} <b>{live && info ? Math.round(info.fps) : 0}{settings ? ` ${$tr('assist.stat.of')} ${settings.fps}` : ''}</b></span>
  <span>{$tr('assist.stat.traffic')} <b>{live && info ? num(info.kbps / 1000) : '0'} {$tr('assist.mbit')}</b>{limit ? ` (${$tr('assist.stat.limit')} ${limit})` : ''}</span>
  <button class="link" type="button" onclick={onsettings}>{$tr('assist.configure')}</button>
</div>

<style>
  .stat {
    display: flex;
    flex-wrap: wrap;
    gap: 2px 14px;
    padding: 4px 10px;
    border-top: 2px solid var(--line);
    background: var(--surface);
    color: var(--muted);
    font-size: 12px;
  }

  b {
    font-weight: 400;
    color: var(--ink);
  }

  .link {
    margin-left: auto;
    padding: 0;
    background: none;
    border: 0;
    color: var(--accent);
    font: inherit;
    text-decoration: underline;
    cursor: pointer;
  }
</style>
