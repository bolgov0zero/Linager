import { readable } from 'svelte/store';

/** The phone layout breakpoint; the same number is used in the `@media (max-width: 720px)` rules. */
export const PHONE_MAX = 720;

/** True while the window is as narrow as a phone (bottom bars, sheets instead of drop-downs). */
export const isPhone = readable(false, (set) => {
  if (typeof matchMedia !== 'function') return;
  const query = matchMedia(`(max-width: ${PHONE_MAX}px)`);
  const update = () => set(query.matches);
  update();
  query.addEventListener('change', update);
  return () => query.removeEventListener('change', update);
});
