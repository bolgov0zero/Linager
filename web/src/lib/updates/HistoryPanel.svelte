<script lang="ts">
  import type { UpdateHistoryRow } from '../../api';
  import { locale, tr as trans } from '../../i18n';
  import { fmtDateTime } from './format';

  let { history }: { history: UpdateHistoryRow[] } = $props();

  const BADGE: Record<UpdateHistoryRow['status'], { cls: string; key: string }> = {
    success: { cls: 'ok', key: 'upd.hist.success' },
    failed: { cls: 'crit', key: 'upd.hist.failed' },
    rolled_back: { cls: 'warn', key: 'upd.hist.rolledBack' },
  };
</script>

<section class="upd-panel">
  <h3 class="upd-h">{$trans('upd.hist.title')}</h3>

  <div class="upd-tw">
    <table class="upd-tbl upd-rows upd-hist">
      <thead>
        <tr>
          <th>{$trans('upd.hist.time')}</th>
          <th>{$trans('upd.hist.what')}</th>
          <th>{$trans('upd.hist.versions')}</th>
          <th>{$trans('upd.hist.result')}</th>
        </tr>
      </thead>
      <tbody>
        {#each history as row (row.id)}
          <tr>
            <td class="upd-sub">{fmtDateTime(row.ts, $trans, $locale)}</td>
            <td>{row.kind === 'server' ? $trans('upd.hist.server') : $trans('upd.hist.agents')}</td>
            <td class="upd-sub upd-mono">{row.from} → {row.to}</td>
            <td>
              <span class="upd-badge {BADGE[row.status].cls}" title={row.detail}>{$trans(BADGE[row.status].key)}</span>
            </td>
          </tr>
        {:else}
          <tr><td colspan="4" class="upd-sub">{$trans('upd.hist.empty')}</td></tr>
        {/each}
      </tbody>
    </table>
  </div>
</section>
