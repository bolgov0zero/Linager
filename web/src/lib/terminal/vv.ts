import { readable } from 'svelte/store';

/** The part of the screen the page can use: the visual viewport shrinks when the on-screen keyboard opens. */
export interface ViewBox {
  /** Distance of the visible area from the top of the layout viewport (px). */
  top: number;
  /** Height of the visible area (px). */
  height: number;
  /** True while a keyboard (or anything else) covers a big part of the layout viewport. */
  kbd: boolean;
}

/** Below this much lost height the change is a toolbar or a bounce, not a keyboard. */
const KBD_MIN = 120;

const read = (): ViewBox => {
  const vv = typeof window !== 'undefined' ? window.visualViewport : null;
  if (!vv) return { top: 0, height: 0, kbd: false };
  const lost = window.innerHeight - vv.height - vv.offsetTop;
  return { top: vv.offsetTop, height: vv.height, kbd: window.innerHeight - vv.height > KBD_MIN && lost >= -1 };
};

/** Follows window.visualViewport; the terminal and the key row stay above the keyboard with it. */
export const viewBox = readable<ViewBox>(read(), (set) => {
  const vv = typeof window !== 'undefined' ? window.visualViewport : null;
  if (!vv) return;
  let last = '';
  const update = () => {
    const v = read();
    const sig = `${v.top}|${v.height}|${v.kbd}`;
    if (sig === last) return;
    last = sig;
    set(v);
  };
  update();
  vv.addEventListener('resize', update);
  vv.addEventListener('scroll', update);
  return () => {
    vv.removeEventListener('resize', update);
    vv.removeEventListener('scroll', update);
  };
});
