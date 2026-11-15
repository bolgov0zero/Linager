import { writable } from 'svelte/store';
import { api, type Snippet } from '../api';

/** The terminal snippets, shared by every administrator. */
export const snippets = writable<Snippet[]>([]);

let loading: Promise<void> | null = null;

/** Reloads the list; concurrent calls share one request. */
export function loadSnippets(): Promise<void> {
  if (!loading) {
    loading = api
      .snippets()
      .then((r) => snippets.set(r.snippets))
      .catch(() => undefined)
      .finally(() => {
        loading = null;
      });
  }
  return loading;
}
