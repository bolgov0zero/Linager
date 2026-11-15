/** Byte sequences for keys the on-screen panel sends to the terminal (xterm / VT220 conventions). */

export interface Mods {
  ctrl: boolean;
  alt: boolean;
}

const ARROWS: Record<string, string> = { Up: 'A', Down: 'B', Right: 'C', Left: 'D', Home: 'H', End: 'F' };
const TILDE: Record<string, number> = { Ins: 2, Del: 3, PgUp: 5, PgDn: 6 };
const FKEYS: Record<string, number> = { F5: 15, F6: 17, F7: 18, F8: 19, F9: 20, F10: 21, F11: 23, F12: 24 };
const F1_4: Record<string, string> = { F1: 'P', F2: 'Q', F3: 'R', F4: 'S' };

/** xterm modifier parameter: 1 + shift(1) + alt(2) + ctrl(4). */
const modParam = (m: Mods) => 1 + (m.alt ? 2 : 0) + (m.ctrl ? 4 : 0);

/** The control character for Ctrl + a printable character, or null when there is none. */
export function ctrlChar(ch: string): string | null {
  if (ch.length !== 1) return null;
  if (ch === ' ') return '\x00';
  if (ch === '?') return '\x7f';
  const code = ch.toUpperCase().charCodeAt(0);
  return code >= 64 && code <= 95 ? String.fromCharCode(code - 64) : null; // @ A-Z [ \ ] ^ _
}

/**
 * The sequence for a named key (Esc, Tab, Up, F5, PgUp ...) or a single character, with the latched Ctrl and Alt.
 * appCursor is the terminal's application cursor mode (full-screen programs such as vim or less set it).
 */
export function sequenceFor(name: string, mods: Mods, appCursor: boolean): string {
  const mod = mods.ctrl || mods.alt;
  if (name === 'Esc') return '\x1b';
  if (name === 'Tab') return mods.alt ? '\x1b\t' : '\t';
  if (name === 'Enter') return mods.alt ? '\x1b\r' : '\r';
  if (name === 'Backspace') return mods.alt ? '\x1b\x7f' : '\x7f';
  const arrow = ARROWS[name];
  if (arrow) return mod ? `\x1b[1;${modParam(mods)}${arrow}` : appCursor ? `\x1bO${arrow}` : `\x1b[${arrow}`;
  const tilde = TILDE[name];
  if (tilde) return mod ? `\x1b[${tilde};${modParam(mods)}~` : `\x1b[${tilde}~`;
  const f14 = F1_4[name];
  if (f14) return mod ? `\x1b[1;${modParam(mods)}${f14}` : `\x1bO${f14}`;
  const f = FKEYS[name];
  if (f) return mod ? `\x1b[${f};${modParam(mods)}~` : `\x1b[${f}~`;
  if (name.length === 1) {
    let out = name;
    if (mods.ctrl) out = ctrlChar(name) ?? name;
    return mods.alt ? `\x1b${out}` : out;
  }
  return '';
}
