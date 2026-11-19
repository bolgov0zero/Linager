/** The wire format of the Assistant socket: every binary message is one frame, a type byte and a payload. */

/** Server to browser. */
export const MSG_HELLO = 1;
export const MSG_TILES = 2;
export const MSG_STATS = 3;
export const MSG_END = 4;
/** Browser to server. */
export const MSG_INPUT = 16;

/** WebSocket close code of the panel when the session is already open in another Assistant window. */
export const ASSIST_LIMIT_CODE = 4031;

export interface Hello { w: number; h: number; session: number | string; user: string }
export interface Stats { fps: number; kbps: number; q: number }
export interface Tile { x: number; y: number; w: number; h: number; jpeg: Uint8Array }

/** The tiles of a Tiles payload: u16 count, then x, y, w, h (u16) and the length (u32, big endian) with the JPEG bytes. Null when the payload is cut or lies about a length. */
export function parseTiles(data: Uint8Array): Tile[] | null {
  if (data.length < 2) return null;
  const view = new DataView(data.buffer, data.byteOffset, data.byteLength);
  const count = view.getUint16(0);
  const tiles: Tile[] = [];
  let at = 2;
  for (let i = 0; i < count; i++) {
    if (at + 12 > data.length) return null;
    const len = view.getUint32(at + 8);
    const start = at + 12;
    if (len > data.length - start) return null;
    tiles.push({ x: view.getUint16(at), y: view.getUint16(at + 2), w: view.getUint16(at + 4), h: view.getUint16(at + 6), jpeg: data.subarray(start, start + len) });
    at = start + len;
  }
  return tiles;
}

/** An input event as a message: the type byte, then JSON. */
export function encodeInput(event: Record<string, unknown>): Uint8Array<ArrayBuffer> {
  const json = new TextEncoder().encode(JSON.stringify(event));
  const out = new Uint8Array(json.length + 1);
  out[0] = MSG_INPUT;
  out.set(json, 1);
  return out;
}

/** Turns wheel deltas into whole notches, keeping the remainder so a smooth trackpad still scrolls. */
export class WheelNotches {
  private fx = 0;
  private fy = 0;

  /** deltaMode follows WheelEvent: 0 pixels (100 a notch), 1 lines (3 a notch), 2 pages (10 notches). Positive is down or right. */
  push(dx: number, dy: number, deltaMode: number): { dx: number; dy: number } {
    const unit = deltaMode === 1 ? 1 / 3 : deltaMode === 2 ? 10 : 1 / 100;
    this.fx += dx * unit;
    this.fy += dy * unit;
    const nx = clampNotch(Math.trunc(this.fx));
    const ny = clampNotch(Math.trunc(this.fy));
    this.fx -= Math.trunc(this.fx);
    this.fy -= Math.trunc(this.fy);
    return { dx: nx, dy: ny };
  }
}

const clampNotch = (n: number): number => Math.max(-10, Math.min(10, n));
