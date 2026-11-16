/**
 * Locks the scrolling of the page behind a full-screen layer (a dialog, a floating window) on a phone.
 * iOS Safari only really stops the page by pinning the body, so the body is fixed at the current scroll offset and the
 * offset is restored afterwards. Counted: nested layers keep the page locked until the last one is gone.
 */
let locks = 0;
let offset = 0;

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
      window.scrollTo(0, offset);
    }
  };
}
