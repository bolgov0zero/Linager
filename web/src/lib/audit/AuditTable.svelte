<script lang="ts">
  import PixIcon from '../ui/PixIcon.svelte';
  import type { AuditItem } from '../../api';
  import { locale, tr as translate } from '../../i18n';
  import { isPhone } from '../ui/viewport';
  import '../ui/phone-rows.css';
  import { KNOWN_ACTIONS } from './actions';

  let { items }: { items: AuditItem[] } = $props();

  // Expanded rows are keyed by audit id, so they survive refreshes and appended pages.
  let expanded = $state<Partial<Record<number, boolean>>>({});

  function toggle(id: number): void {
    expanded[id] = !expanded[id];
  }

  function formatTime(ts: number, lang: string): string {
    return new Date(ts * 1000).toLocaleString(lang === 'en' ? 'en-GB' : 'ru-RU', {
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
    });
  }

  /** Phones: a short time, "16 нояб., 10:12". */
  function shortTime(ts: number, lang: string): string {
    return new Date(ts * 1000).toLocaleString(lang === 'en' ? 'en-GB' : 'ru-RU', {
      day: 'numeric',
      month: 'short',
      hour: '2-digit',
      minute: '2-digit',
    });
  }

  function actionText(action: string): string {
    return KNOWN_ACTIONS.has(action) ? $translate(`audit.action.${action}`) : action;
  }

  function resultClass(result: string): 'ok' | 'failed' | 'other' {
    if (result === 'ok') return 'ok';
    if (result === 'failed') return 'failed';
    return 'other';
  }

  function resultText(result: string): string {
    if (result === 'ok') return $translate('audit.result.ok');
    if (result === 'failed') return $translate('audit.result.failed');
    return result;
  }
</script>

{#if $isPhone}
  <div class="pr-list">
    {#each items as item (item.id)}
      {@const isOpen = !!expanded[item.id]}
      {@const rc = resultClass(item.result)}
      <div class="pr-it">
        <button
          class="pr-li"
          class:bad={rc === 'failed'}
          class:wn={rc === 'other'}
          type="button"
          aria-expanded={isOpen}
          onclick={() => toggle(item.id)}
        >
          <div class="pr-mn">
            <div class="pr-l1">
              <b>{actionText(item.action)}</b>
              {#if rc !== 'ok'}<span class="pr-tag {rc === 'failed' ? 'crit' : 'warn'}">{resultText(item.result)}</span>{/if}
            </div>
            <div class="pr-l2">{item.actor || '—'}{#if item.target_name}{' · ' + item.target_name}{/if} · {shortTime(item.ts, $locale)}</div>
          </div>
          <span class="chev" class:open={isOpen} aria-hidden="true"></span>
        </button>
        {#if isOpen}
          <div class="pr-more">
            <div class="pr-kv"><span>{$translate('audit.col.time')}</span><span class="mono">{formatTime(item.ts, $locale)}</span></div>
            {#if item.target_type}
              <div class="pr-kv"><span>{$translate('audit.col.target')}</span><span>{item.target_type}{#if item.target_name}{' · ' + item.target_name}{/if}</span></div>
            {/if}
            <div class="pr-kv">
              <span>{$translate('audit.col.ip')}</span>
              <span class="mono">
                {item.client_ip || '—'}{#if item.remote_addr && item.remote_addr !== item.client_ip} <span class="muted">({item.remote_addr})</span>{/if}
              </span>
            </div>
            {#if item.details}<pre class="json">{JSON.stringify(item.details, null, 2)}</pre>{/if}
          </div>
        {/if}
      </div>
    {/each}
  </div>
{:else}
<div class="table-wrap">
  <table>
    <thead>
      <tr>
        <th scope="col">{$translate('audit.col.time')}</th>
        <th scope="col">{$translate('audit.col.actor')}</th>
        <th scope="col">{$translate('audit.col.action')}</th>
        <th scope="col">{$translate('audit.col.target')}</th>
        <th scope="col">{$translate('audit.col.result')}</th>
        <th scope="col">{$translate('audit.col.ip')}</th>
        <th scope="col"><span class="sr">{$translate('audit.col.details')}</span></th>
      </tr>
    </thead>
    <tbody>
      {#each items as item (item.id)}
        {@const isOpen = !!expanded[item.id]}
        <tr>
          <td class="nowrap mono">{formatTime(item.ts, $locale)}</td>
          <td>{item.actor || '—'}</td>
          <td>{actionText(item.action)}</td>
          <td>
            <div>{item.target_name || '—'}</div>
            {#if item.target_type}
              <div class="small muted">{item.target_type}</div>
            {/if}
          </td>
          <td>
            <span class="badge {resultClass(item.result)}">{resultText(item.result)}</span>
          </td>
          <td class="mono">
            {#if item.remote_addr && item.remote_addr !== item.client_ip}
              <span title={$translate('audit.remoteTitle', { addr: item.remote_addr })}>
                {item.client_ip || '—'}
                <span class="small muted">{item.remote_addr}</span>
              </span>
            {:else}
              {item.client_ip || '—'}
            {/if}
          </td>
          <td class="toggle-cell">
            <button
              class="toggle"
              type="button"
              aria-expanded={isOpen}
              aria-label={isOpen ? $translate('audit.collapse') : $translate('audit.expand')}
              title={isOpen ? $translate('audit.collapse') : $translate('audit.expand')}
              disabled={!item.details}
              onclick={() => toggle(item.id)}
            ><PixIcon name={isOpen ? 'chev' : 'next'} size={12} /></button>
          </td>
        </tr>
        {#if isOpen && item.details}
          <tr>
            <td colspan="7" class="details">
              <pre class="json">{JSON.stringify(item.details, null, 2)}</pre>
            </td>
          </tr>
        {/if}
      {/each}
    </tbody>
  </table>
</div>
{/if}

<style>
  .table-wrap {
    overflow-x: auto;
  }

  table {
    width: 100%;
    border-collapse: collapse;
    font-size: 11px;
  }

  th,
  td {
    padding: 7px 9px;
    text-align: left;
    vertical-align: middle;
    border-bottom: 2px dashed var(--seg-off);
  }

  th {
    color: var(--muted);
    font-weight: 400;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    white-space: nowrap;
    border-bottom: 2px solid var(--line);
  }

  .nowrap {
    white-space: nowrap;
  }

  .sr {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip: rect(0 0 0 0);
    white-space: nowrap;
  }

  .small {
    font-size: 10px;
  }

  .muted {
    color: var(--muted);
  }

  .mono {
    letter-spacing: 0.04em;
    overflow-wrap: anywhere;
  }

  .badge {
    display: inline-block;
    padding: 1px 5px;
    border: 2px solid currentColor;
    white-space: nowrap;
  }

  .badge.ok {
    color: var(--good);
  }

  .badge.failed {
    color: var(--crit);
  }

  .badge.other {
    color: var(--warn);
  }

  .toggle-cell {
    width: 1%;
  }

  .toggle {
    background: var(--surface);
    color: var(--ink);
    border: 2px solid var(--line);
    padding: 0 5px;
    line-height: 1.4;
    cursor: pointer;
    font: inherit;
  }

  .toggle:disabled {
    opacity: 0.35;
    cursor: default;
  }

  .toggle:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }

  td.details {
    padding: 0 9px 9px;
  }

  .json {
    margin: 0;
    padding: 9px 11px;
    background: #0b0d12;
    color: #d7dce6;
    font: inherit;
    font-size: 11px;
    line-height: 1.5;
    overflow-x: auto;
    white-space: pre;
  }

  .chev {
    flex: none;
    width: 0;
    height: 0;
    margin: 0 5px;
    border-left: 5px solid transparent;
    border-right: 5px solid transparent;
    border-top: 6px solid var(--muted);
    transition: transform 0.15s;
  }

  .chev.open {
    transform: rotate(180deg);
  }

  .pr-more .json {
    margin-top: 7px;
  }
</style>
