/**
 * Locks the scrolling of the page behind a full-screen layer (a dialog, a floating window) on a phone.
 * iOS Safari only really stops the page by pinning the body, so the body is fixed at the current scroll offset and the
 * offset is restored afterwards. Counted: nested layers keep the page locked until the last one is gone.
 */
let locks = 0;
let offset = 0;
let startY = 0;

/** The nearest ancestor that can scroll vertically, or null when the finger is over something that cannot. */
function scroller(from: EventTarget | null): HTMLElement | null {
  for (let el = from as HTMLElement | null; el && el !== document.body; el = el.parentElement) {
    if (el.scrollHeight <= el.clientHeight + 1) continue;
    const overflow = getComputedStyle(el).overflowY;
    if (overflow === 'auto' || overflow === 'scroll') return el;
  }
  return null;
}

function onTouchStart(e: TouchEvent): void {
  startY = e.touches[0]?.clientY ?? 0;
}

/**
 * iOS lets a gesture that cannot scroll anything move the page (or bounce it) even when the body is pinned. While a
 * layer is open, a move is only allowed where an element under the finger really scrolls in that direction.
 */
function onTouchMove(e: TouchEvent): void {
  if (e.touches.length !== 1) return;
  const el = scroller(e.target);
  if (!el) {
    e.preventDefault();
    return;
  }
  const dy = (e.touches[0]?.clientY ?? 0) - startY;
  const atTop = el.scrollTop <= 0;
  const atBottom = el.scrollTop + el.clientHeight >= el.scrollHeight - 1;
  if ((dy > 0 && atTop) || (dy < 0 && atBottom)) e.preventDefault();
}

export function lockScroll(): () => void {
  if (typeof document === 'undefined') return () => {};
  if (locks === 0) {
    offset = window.scrollY;
    const body = document.body;
    body.style.position = 'fixed';
    body.style.top = `${-offset}px`;
    body.style.left = '0';
    body.style.right = '0';
    body.style.width = '100%';
    document.addEventListener('touchstart', onTouchStart, { passive: true });
    document.addEventListener('touchmove', onTouchMove, { passive: false });
  }
  locks += 1;
  let released = false;
  return () => {
    if (released) return;
    released = true;
    locks -= 1;
    if (locks === 0) {
      const body = document.body;
      body.style.position = '';
      body.style.top = '';
      body.style.left = '';
      body.style.right = '';
      body.style.width = '';
      document.removeEventListener('touchstart', onTouchStart);
      document.removeEventListener('touchmove', onTouchMove);
      window.scrollTo(0, offset);
    }
  };
}
