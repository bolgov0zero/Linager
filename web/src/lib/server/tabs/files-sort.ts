import type { FileEntry } from '../../../api';

export type SortKey = 'name' | 'mode' | 'owner' | 'size' | 'mtime';
export type SortDir = 'asc' | 'desc';

/** Directories always come first; inside each group the rows follow the chosen column (the name breaks ties). */
export function sortEntries(entries: FileEntry[] | null | undefined, key: SortKey = 'name', dir: SortDir = 'asc'): FileEntry[] {
  const rank = (e: FileEntry) => (e.type === 'dir' ? 0 : 1);
  const byName = (a: FileEntry, b: FileEntry) => a.name.localeCompare(b.name);
  const sign = dir === 'asc' ? 1 : -1;
  const by = (a: FileEntry, b: FileEntry): number => {
    switch (key) {
      case 'mode':
        return a.mode_str.localeCompare(b.mode_str);
      case 'owner':
        return `${a.owner}:${a.group}`.localeCompare(`${b.owner}:${b.group}`);
      case 'size':
        return a.size - b.size;
      case 'mtime':
        return a.mtime - b.mtime;
      default:
        return byName(a, b);
    }
  };
  return [...(entries ?? [])].sort((a, b) => rank(a) - rank(b) || sign * by(a, b) || byName(a, b));
}
