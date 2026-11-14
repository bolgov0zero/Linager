<script lang="ts">
  import type { UpdatesInfo } from '../../api';
  import { locale, tr as trans } from '../../i18n';
  import { fmtBytes, fmtDate } from './format';

  let {
    cache,
    prefetching,
    onprefetch,
  }: {
    cache: UpdatesInfo['cache'];
    /** Version currently being downloaded, or null. */
    prefetching: string | null;
    onprefetch: (version: string) => void;
  } = $props();
</script>

<section class="upd-panel">
  <h3 class="upd-h">{$trans('upd.cache.title')}</h3>

  <div class="upd-tw">
    <table class="upd-tbl">
      <thead>
        <tr>
          <th>{$trans('upd.cache.version')}</th>
          <th>{$trans('upd.cache.size')}</th>
          <th>{$trans('upd.cache.loaded')}</th>
          <th>{$trans('upd.cache.sign')}</th>
        </tr>
      </thead>
      <tbody>
        {#each cache as entry (entry.version)}
          <tr>
            <td class="upd-mono"><b>{entry.version}</b></td>
            <td class="upd-sub">{entry.downloaded ? fmtBytes(entry.size, $trans) : '—'}</td>
            <td class="upd-sub">{entry.downloaded ? fmtDate(entry.downloaded_at, $locale) : $trans('upd.cache.notLoaded')}</td>
            <td>
              {#if !entry.downloaded}
                <button
                  class="btn sm"
                  type="button"
                  disabled={prefetching !== null}
                  onclick={() => onprefetch(entry.version)}
                >
                  {#if prefetching === entry.version}
                    <span class="upd-spin" aria-hidden="true"></span> {$trans('upd.cache.prefetching')}
                  {:else}
                    {$trans('upd.cache.prefetch')}
                  {/if}
                </button>
              {:else if entry.verified}
                <span class="upd-badge ok">{$trans('upd.cache.verified')}</span>
              {:else}
                <span class="upd-sub">—</span>
              {/if}
            </td>
          </tr>
        {:else}
          <tr><td colspan="4" class="upd-sub">{$trans('upd.cache.empty')}</td></tr>
        {/each}
      </tbody>
    </table>
  </div>

  <p class="upd-hint">{$trans('upd.cache.note')}</p>
</section>
