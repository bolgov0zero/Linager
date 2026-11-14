import { writable } from 'svelte/store';

export const toast = writable<string | null>(null);

let timer: ReturnType<typeof setTimeout> | undefined;

/** Shows a short message for a few seconds. Pass an already translated string. */
export function showToast(message: string): void {
  toast.set(message);
  clearTimeout(timer);
  timer = setTimeout(() => toast.set(null), 3500);
}
