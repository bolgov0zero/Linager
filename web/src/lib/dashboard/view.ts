import { writable } from 'svelte/store';

export type DashView = 'cards' | 'map' | 'pult';

const VIEW_KEY = 'linager.dashboard.view';

function readView(): DashView {
  try {
    const value = localStorage.getItem(VIEW_KEY);
    if (value === 'cards' || value === 'pult') return value;
    if (value === 'map' || value === 'rows') return 'map'; // the old rows view was replaced by the map
  } catch {
    // storage unavailable: fall back to the default view
  }
  return 'cards';
}

/** The dashboard view: chosen in the top bar, drawn by the dashboard. */
export const dashView = writable<DashView>(readView());

export function setDashView(next: DashView): void {
  dashView.set(next);
  try {
    localStorage.setItem(VIEW_KEY, next);
  } catch {
    // storage unavailable: the choice just is not remembered
  }
}
