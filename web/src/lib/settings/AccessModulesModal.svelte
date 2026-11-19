<script lang="ts" module>
  /** The agent modules an operator can get, and the ones that give the access of root on the host. */
  export const MODULES = ['terminal', 'desktop', 'assistant', 'files', 'services', 'apt', 'processes', 'logs', 'disk', 'network', 'firewall', 'security', 'users', 'docker'] as const;
  export const ROOT_MODULES: readonly string[] = ['terminal', 'desktop', 'assistant', 'files', 'network', 'firewall', 'security', 'users', 'docker'];
</script>

<script lang="ts">
  import { untrack } from 'svelte';
  import { tr } from '../../i18n';
  import Modal from '../Modal.svelte';

  let {
    selected,
    onsave,
    onclose,
  }: {
    selected: string[];
    onsave: (modules: string[]) => void;
    onclose: () => void;
  } = $props();

  let picked = $state<string[]>(untrack(() => [...selected]));

  const tabKey = (m: string): string => (m === 'apt' ? 'srv.tab.updates' : `srv.tab.${m}`);
  const rootPicked = $derived(picked.filter((m) => ROOT_MODULES.includes(m)));

  function toggle(m: string): void {
    picked = picked.includes(m) ? picked.filter((x) => x !== m) : [...picked, m];
  }
</script>

<Modal title={$tr('acc.modules.title')} labelId="acc-modules-title" {onclose}>
  <p class="muted small">{$tr('acc.modules.hint')}</p>
  <ul class="list">
    {#each MODULES as m (m)}
      <li class="mod">
        <button class="sw" type="button" aria-pressed={picked.includes(m)} aria-label={$tr(tabKey(m))} onclick={() => toggle(m)}></button>
        <div class="txt">
          <strong>{$tr(tabKey(m))}</strong>
          <span class="muted small">{$tr(`acc.mod.${m}`)}</span>
        </div>
        {#if ROOT_MODULES.includes(m)}<span class="badge root">{$tr('acc.modules.root')}</span>{/if}
      </li>
    {/each}
  </ul>
  {#if rootPicked.length > 0}
    <p class="warn small" role="status">{$tr('acc.modules.rootWarn')}</p>
  {/if}
  <div class="row between bar">
    <span class="small muted">{$tr('acc.modules.total', { n: picked.length })}</span>
    <div class="row">
      <button class="btn" type="button" onclick={onclose}>{$tr('common.cancel')}</button>
      <button class="btn primary" type="button" onclick={() => onsave(MODULES.filter((m) => picked.includes(m)))}>{$tr('acc.apply')}</button>
    </div>
  </div>
</Modal>

<style>
  .list {
    margin: 0;
    padding: 0;
    list-style: none;
    border: 2px solid var(--line);
    max-height: min(52vh, 414px);
    overflow: auto;
  }

  .mod {
    display: grid;
    grid-template-columns: auto 1fr auto;
    gap: 11px;
    align-items: center;
    padding: 8px 11px;
    border-bottom: 1px dashed var(--seg-off);
  }

  .mod:last-child {
    border-bottom: 0;
  }

  .txt {
    display: flex;
    flex-direction: column;
    gap: 1px;
    min-width: 0;
  }

  .badge.root {
    padding: 1px 5px;
    border: 2px solid var(--warn);
    color: var(--warn);
    font-size: 11px;
    white-space: nowrap;
  }

  .warn {
    padding: 7px 11px;
    border: 2px solid var(--warn);
    color: var(--warn);
  }

  .sw {
    position: relative;
    width: 34px;
    height: 20px;
    padding: 0;
    border: 2px solid var(--line);
    background: var(--seg-off);
    cursor: pointer;
  }

  .sw::after {
    content: '';
    position: absolute;
    top: 2px;
    left: 2px;
    width: 13px;
    height: 13px;
    background: var(--ink);
    transition: transform 0.15s;
  }

  .sw[aria-pressed='true'] {
    background: var(--good);
  }

  .sw[aria-pressed='true']::after {
    transform: translateX(16px);
    background: var(--surface);
  }

  p {
    margin: 0;
  }

  /* Phones: the sheet scrolls as a whole, the switches get a bigger tap area, the buttons stay at the bottom. */
  @media (max-width: 720px) {
    .list {
      max-height: none;
      overflow: visible;
    }

    .sw::before {
      content: '';
      position: absolute;
      inset: -10px -5px;
    }

    .bar {
      position: sticky;
      bottom: 0;
      z-index: 2;
      flex-direction: column;
      align-items: stretch;
      gap: 7px;
      margin: 0 -11px calc(-11px - var(--safe-b));
      padding: 9px 11px calc(9px + var(--safe-b));
      background: var(--surface);
      border-top: 2px solid var(--line);
    }

    .bar > .row {
      flex-wrap: nowrap;
    }

    .bar > .row > .btn {
      flex: 1;
    }

    .mod {
      min-height: 48px;
    }
  }
</style>
