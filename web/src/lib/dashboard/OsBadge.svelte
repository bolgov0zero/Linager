<script lang="ts">
  import { osKind } from './status';

  /** The platform of a server, in the corner of its card: the names are the products' own, so they are not translated. */
  let { os, pushed = true }: { os: string; /** Push the badge to the right edge (in the edit mode the edit buttons do it). */ pushed?: boolean } = $props();
  const kind = $derived(osKind(os));
</script>

<span class="os mono" class:pushed class:win={kind === 'windows'} title={os}>{kind === 'windows' ? 'Windows' : 'Linux'}</span>

<style>
  .os {
    flex: none;
    padding: 1px 6px;
    border: 2px solid var(--line);
    color: var(--muted);
    background: var(--surface);
    font-size: 11px;
    line-height: 1.3;
    letter-spacing: 0.04em;
    text-transform: uppercase;
  }

  .os.pushed {
    margin-left: auto;
  }

  .os.win {
    border-color: var(--accent);
    color: var(--accent);
  }
</style>
