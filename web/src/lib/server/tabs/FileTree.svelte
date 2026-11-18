<script lang="ts">
  import { untrack } from 'svelte';
  import { api, type FileEntry } from '../../../api';
  import { tr } from '../../../i18n';
  import { crumbs, joinPath } from './files-util';

  // Folder tree for the files tab. Folders load lazily on expand; the listing the main pane already
  // fetched for the current folder is reused, so navigation costs no extra request.
  let {
    serverId,
    path,
    entries,
    onnavigate,
  }: {
    serverId: string;
    /** Folder shown in the main pane. */
    path: string;
    /** Its entries (null while not loaded). */
    entries: FileEntry[] | null;
    onnavigate: (path: string) => void;
  } = $props();

  interface Node {
    children: string[] | null; // child folder names; null = not loaded
    loading: boolean;
    failed: boolean;
  }

  let nodes = $state<Record<string, Node>>({});
  let expanded = $state<Record<string, boolean>>({ '/': true });

  function dirNames(list: FileEntry[]): string[] {
    return list
      .filter((e) => e.type === 'dir')
      .map((e) => e.name)
      .sort((a, b) => a.localeCompare(b));
  }

  async function load(dir: string): Promise<void> {
    const node = nodes[dir];
    if (node?.loading || node?.children) return;
    nodes[dir] = { children: null, loading: true, failed: false };
    try {
      const res = await api.files(serverId, dir);
      nodes[dir] = { children: dirNames(res.entries ?? []), loading: false, failed: false };
    } catch {
      nodes[dir] = { children: null, loading: false, failed: true };
    }
  }

  // A new server starts a new tree.
  $effect(() => {
    void serverId;
    untrack(() => {
      nodes = {};
      expanded = { '/': true };
    });
  });

  // The current folder: take its children from the main listing, open its ancestors and load them.
  $effect(() => {
    const dir = path;
    const list = entries;
    untrack(() => {
      if (list) nodes[dir] = { children: dirNames(list), loading: false, failed: false };
      for (const p of ['/', ...crumbs(dir).map((c) => c.path)]) {
        expanded[p] = true;
        void load(p);
      }
    });
  });

  function toggle(dir: string): void {
    expanded[dir] = !expanded[dir];
    if (expanded[dir]) void load(dir);
  }

  /** Flat list of visible rows, depth-first. */
  const rows = $derived.by(() => {
    const out: { path: string; name: string; depth: number }[] = [];
    const walk = (dir: string, name: string, depth: number) => {
      out.push({ path: dir, name, depth });
      if (!expanded[dir]) return;
      for (const child of nodes[dir]?.children ?? []) walk(joinPath(dir, child), child, depth + 1);
    };
    walk('/', '/', 0);
    return out;
  });
</script>

<ul class="tree" role="tree" aria-label={$tr('files.tree')}>
  {#each rows as row (row.path)}
    {@const node = nodes[row.path]}
    {@const open = !!expanded[row.path]}
    {@const leaf = node?.children?.length === 0}
    <li
      role="treeitem"
      aria-selected={row.path === path}
      aria-expanded={leaf ? undefined : open}
      class:current={row.path === path}
      style:--depth={row.depth}
    >
      <button
        class="twist"
        type="button"
        tabindex="-1"
        aria-label={open ? $tr('files.collapse') : $tr('files.expand')}
        disabled={leaf}
        onclick={() => toggle(row.path)}
      >{leaf ? '' : node?.loading ? '·' : open ? '▾' : '▸'}</button>
      <button class="label" type="button" title={row.path} onclick={() => onnavigate(row.path)} ondblclick={() => toggle(row.path)}>
        <span class="icon" aria-hidden="true"></span><span class="text">{row.name}</span>
      </button>
      {#if node?.failed}<span class="muted small" title={$tr('files.treeFailed')}>!</span>{/if}
    </li>
  {/each}
</ul>

<style>
  .tree {
    margin: 0;
    padding: 0;
    list-style: none;
    font-size: 12px;
  }

  li {
    display: flex;
    align-items: center;
    min-width: 0;
    padding-left: calc(var(--depth) * 13px);
    white-space: nowrap;
  }

  .twist {
    flex: none;
    width: 16px;
    padding: 0;
    background: none;
    border: 0;
    color: var(--muted);
    cursor: pointer;
    font: inherit;
  }

  .twist:disabled {
    cursor: default;
  }

  .label {
    display: flex;
    align-items: center;
    gap: 5px;
    min-width: 0;
    padding: 3px 5px;
    background: none;
    border: 2px solid transparent;
    color: var(--ink);
    cursor: pointer;
    font: inherit;
    text-align: left;
  }

  .label:hover .text {
    color: var(--accent);
  }

  .label:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
  }

  li.current .label {
    border-color: var(--line);
    background: var(--seg-off);
  }

  .icon {
    flex: none;
    width: 9px;
    height: 9px;
    background: var(--accent);
    border: 2px solid var(--accent);
  }

  .text {
    overflow: hidden;
    text-overflow: ellipsis;
  }

  /* Phones (a list in a sheet): every folder is a full-width row of at least 46 px, indented by its depth. */
  @media (max-width: 720px) {
    .tree {
      font-size: 14px;
    }

    li {
      min-height: 46px;
      padding-left: calc(4px + var(--depth) * 16px);
      border-top: 2px dashed var(--seg-off);
    }

    li:first-child {
      border-top: 0;
    }

    .twist {
      width: 32px;
      height: 44px;
      font-size: 16px;
    }

    .label {
      flex: 1;
      min-height: 44px;
      padding: 0 7px;
    }

    .icon {
      width: 11px;
      height: 11px;
    }
  }
</style>
