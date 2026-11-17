// The scheme view of the dashboard is a plane: x and y are pixels at 100% zoom and the panel stands at 0,0 until it
// is moved. The automatic layout puts the servers on rings around the panel and grows the rings until nothing
// touches, so a node never shrinks; the view (zoom and shift) is what makes a large fleet fit on the screen.

export const PANEL_W = 190;
export const PANEL_H = 74;
export const MIN_ZOOM = 0.2;
export const MAX_ZOOM = 2.5;
/** Up to this many servers a node is shown in full (name and status), above it in the compact form (name only). */
export const FULL_MAX = 16;

export type NodeMode = 'full' | 'compact';
export interface Pt {
  x: number;
  y: number;
}
/** Left, top, right, bottom of a box on the plane. */
export type Box = [number, number, number, number];
export interface AutoLayout {
  mode: NodeMode;
  nw: number;
  nh: number;
  /** One place per server, in the order of the list that was laid out. */
  pos: Pt[];
}
export interface View {
  k: number;
  tx: number;
  ty: number;
}

export function clamp(v: number, lo: number, hi: number): number {
  return Math.max(lo, Math.min(hi, v));
}

/** Dragged things snap to a 10 px grid. */
export function snap(v: number): number {
  return Math.round(v / 10) * 10;
}

/** The speed of the squares on a link by the sum of the receive and send rates: slow below 50 KB/s, medium below 2 MB/s. */
export function speedLevel(bytesPerSec: number): 1 | 2 | 3 {
  return bytesPerSec < 50e3 ? 1 : bytesPerSec < 2e6 ? 2 : 3;
}

function ringLength(a: number, b: number): number {
  // Ramanujan's approximation of the perimeter of an ellipse with half-axes a and b
  return Math.PI * (3 * (a + b) - Math.sqrt((3 * a + b) * (a + 3 * b)));
}

/** True when two nodes (with a margin m) or a node and the panel overlap. */
export function overlaps(pos: Pt[], nw: number, nh: number, m: number): boolean {
  const panel: Box = [-PANEL_W / 2 - 14, -PANEL_H / 2 - 14, PANEL_W / 2 + 14, PANEL_H / 2 + 14];
  const boxes = pos.map((q): Box => [q.x - nw / 2 - m, q.y - nh / 2 - m, q.x + nw / 2 + m, q.y + nh / 2 + m]);
  for (let i = 0; i < boxes.length; i++) {
    const a = boxes[i] as Box;
    if (a[0] < panel[2] && a[2] > panel[0] && a[1] < panel[3] && a[3] > panel[1]) return true;
    for (let j = i + 1; j < boxes.length; j++) {
      const b = boxes[j] as Box;
      if (a[0] < b[2] && a[2] > b[0] && a[1] < b[3] && a[3] > b[1]) return true;
    }
  }
  return false;
}

/** Places n servers on rings around the panel. The rings are made larger until no two nodes touch. */
export function autoLayout(n: number): AutoLayout {
  const full = n <= FULL_MAX;
  const nw = full ? 160 : 118;
  const nh = full ? 58 : 30;
  const rings = full ? 1 : Math.min(8, Math.ceil(n / 18));
  const rx0 = PANEL_W / 2 + nw / 2 + 50;
  const ry0 = PANEL_H / 2 + nh / 2 + 50;
  const sx = nw + 34;
  const sy = nh + 34;
  let pos: Pt[] = [];
  // A few full nodes start on a wider ring so that the frames of the groups do not run into each other.
  for (let s = full ? 1.7 : 1; s < 200; s *= 1.05) {
    const lengths: number[] = [];
    let total = 0;
    for (let k = 0; k < rings; k++) {
      const len = ringLength((rx0 + k * sx) * s, (ry0 + k * sy) * s);
      lengths.push(len);
      total += len;
    }
    const counts: number[] = [];
    let left = n;
    for (let k = 0; k < rings; k++) {
      const c = k === rings - 1 ? left : Math.min(left, Math.round((n * (lengths[k] as number)) / total));
      counts.push(c);
      left -= c;
    }
    pos = [];
    for (let k = 0; k < rings; k++) {
      const m = counts[k] as number;
      const phase = ((k % 2) * Math.PI) / Math.max(m, 1);
      for (let i = 0; i < m; i++) {
        const a = phase + (2 * Math.PI * i) / m - Math.PI / 2;
        pos.push({ x: Math.round((rx0 + k * sx) * s * Math.cos(a)), y: Math.round((ry0 + k * sy) * s * Math.sin(a)) });
      }
    }
    if (!overlaps(pos, nw, nh, 8)) break;
  }
  return { mode: full ? 'full' : 'compact', nw, nh, pos };
}

/** The view that shows the whole box with a margin; it never zooms in beyond 100%. */
export function fitView(b: Box, w: number, h: number): View {
  const m = 36;
  const bw = Math.max(1, b[2] - b[0]);
  const bh = Math.max(1, b[3] - b[1]);
  const k = clamp(Math.min(1, (w - 2 * m) / bw, (h - 2 * m) / bh), MIN_ZOOM, MAX_ZOOM);
  return { k, tx: w / 2 - (b[0] + bw / 2) * k, ty: h / 2 - (b[1] + bh / 2) * k };
}

/** Zooms by a factor around a point of the screen, so that the point stays where it is. */
export function zoomAround(v: View, px: number, py: number, factor: number): View {
  const k = clamp(v.k * factor, MIN_ZOOM, MAX_ZOOM);
  const r = k / v.k;
  return { k, tx: px - (px - v.tx) * r, ty: py - (py - v.ty) * r };
}

// ---- links with right-angle bends ----

export type LinkStyle = 'straight' | 'bus' | 'route';
export const LINK_STYLES: readonly LinkStyle[] = ['straight', 'bus', 'route'];
/**
 * The router is a preference for a readable picture. It is used while the nodes are shown in full: the rings of the
 * compact form are packed so close that the panel is walled in and most links would have no way through, so above
 * this many servers the tracks of busPath are used.
 */
export const ROUTE_MAX = FULL_MAX;

/**
 * A link with right-angle bends drawn like a bus on a circuit board: out of the node along the longer axis to one of
 * three parallel tracks that run around the panel, along the track, then into the panel at a point of its own.
 * It is cheap and never fails, but it does not look at other nodes.
 */
export function busPath(a: Pt, b: Pt, i: number): string {
  const dx = b.x - a.x;
  const dy = b.y - a.y;
  const gap = 46;
  const track = (i % 3) * 12;
  if (Math.abs(dx) >= Math.abs(dy)) {
    const side = dx > 0 ? -1 : 1;
    const bx = b.x + side * (PANEL_W / 2 + gap + track);
    const ey = b.y + ((i % 5) - 2) * 12;
    return `M ${a.x} ${a.y} L ${bx} ${a.y} L ${bx} ${ey} L ${b.x} ${ey}`;
  }
  const sd = dy > 0 ? -1 : 1;
  const by = b.y + sd * (PANEL_H / 2 + gap + track);
  const ex = b.x + ((i % 7) - 3) * 22;
  return `M ${a.x} ${a.y} L ${a.x} ${by} L ${ex} ${by} L ${ex} ${b.y}`;
}

export interface RouteNode extends Pt {
  id: string;
}

/**
 * Right-angle links that keep away from the nodes and from each other where that is cheap. The plane is cut into
 * cells; every node and the panel is a wall with a margin; a link is found by Dijkstra over (cell, direction).
 * Costs: a step 1, a turn 6, running next to a wall 2, running along a link already laid 6, crossing one 14, ending at
 * an entry slot of the panel that is taken 25 per link. The links are laid shortest first, so the short ones keep the
 * direct way and the long ones go around. It is a preference and not a promise: a link for which no way is found within
 * the budget gets null, and the caller draws the tracks of busPath instead.
 */
export function routeLinks(nodes: RouteNode[], nw: number, nh: number, panel: Pt, bounds: Box): Record<string, string | null> {
  const N = nodes.length;
  const out: Record<string, string | null> = {};
  if (N === 0) return out;
  const step = N > 16 ? 20 : 10;
  const pad = 150;
  const x0 = Math.floor((bounds[0] - pad) / step) * step;
  const y0 = Math.floor((bounds[1] - pad) / step) * step;
  const gw = Math.ceil((bounds[2] + pad - x0) / step) + 1;
  const gh = Math.ceil((bounds[3] + pad - y0) / step) + 1;
  const G = gw * gh;
  const blk = new Uint8Array(G);
  const nr = new Uint8Array(G);
  const hU = new Uint8Array(G);
  const vU = new Uint8Array(G);
  const lo = (v: number, o: number) => Math.floor((v - o) / step);
  const hi = (v: number, o: number) => Math.ceil((v - o) / step);
  const mark = (b: Box, m: number, arr: Uint8Array) => {
    const a = Math.max(0, lo(b[0] - m, x0));
    const c = Math.min(gw - 1, hi(b[2] + m, x0));
    const d = Math.max(0, lo(b[1] - m, y0));
    const e = Math.min(gh - 1, hi(b[3] + m, y0));
    for (let y = d; y <= e; y++) for (let x = a; x <= c; x++) arr[y * gw + x] = 1;
  };
  const boxes = nodes.map((q): Box => [q.x - nw / 2, q.y - nh / 2, q.x + nw / 2, q.y + nh / 2]);
  const pb: Box = [panel.x - PANEL_W / 2, panel.y - PANEL_H / 2, panel.x + PANEL_W / 2, panel.y + PANEL_H / 2];
  for (const b of boxes) {
    mark(b, 8, blk);
    mark(b, 8 + step, nr);
  }
  mark(pb, 8, blk);
  mark(pb, 8 + step, nr);

  // Five entry slots on every side of the panel; the goal is the cell in front of a slot, reached moving inwards.
  const DX = [1, -1, 0, 0]; // E W S N
  const DY = [0, 0, 1, -1];
  const goal = new Map<number, number>();
  const slotUse: number[] = new Array(20).fill(0);
  const slot: { edge: number; at: number }[] = new Array(20);
  const addGoal = (side: number, j: number, cx: number, cy: number, dir: number, edge: number, at: number) => {
    if (cx < 0 || cy < 0 || cx >= gw || cy >= gh || blk[cy * gw + cx]) return;
    goal.set((cy * gw + cx) * 4 + dir, side * 5 + j);
    slot[side * 5 + j] = { edge, at };
  };
  for (let j = 0; j < 5; j++) {
    const yS = panel.y + (j - 2) * ((PANEL_H - 18) / 4);
    const xS = panel.x + (j - 2) * ((PANEL_W - 30) / 4);
    addGoal(0, j, lo(pb[0] - 8, x0) - 1, Math.round((yS - y0) / step), 0, pb[0], yS);
    addGoal(1, j, hi(pb[2] + 8, x0) + 1, Math.round((yS - y0) / step), 1, pb[2], yS);
    addGoal(2, j, Math.round((xS - x0) / step), lo(pb[1] - 8, y0) - 1, 2, pb[1], xS);
    addGoal(3, j, Math.round((xS - x0) / step), hi(pb[3] + 8, y0) + 1, 3, pb[3], xS);
  }

  const dist = new Float32Array(G * 4);
  const prev = new Int32Array(G * 4);
  const order = nodes
    .map((_, i) => i)
    .sort((a, b) => {
      const qa = nodes[a] as RouteNode;
      const qb = nodes[b] as RouteNode;
      return Math.abs(qa.x - panel.x) + Math.abs(qa.y - panel.y) - (Math.abs(qb.x - panel.x) + Math.abs(qb.y - panel.y));
    });

  for (const i of order) {
    const node = nodes[i] as RouteNode;
    const b = boxes[i] as Box;
    dist.fill(Infinity);
    prev.fill(-1);
    // a binary heap of (cost, state)
    const hc: number[] = [];
    const hs: number[] = [];
    const push = (c: number, st: number) => {
      let k = hc.length;
      hc.push(c);
      hs.push(st);
      while (k > 0) {
        const p = (k - 1) >> 1;
        if ((hc[p] as number) <= (hc[k] as number)) break;
        [hc[p], hc[k]] = [hc[k] as number, hc[p] as number];
        [hs[p], hs[k]] = [hs[k] as number, hs[p] as number];
        k = p;
      }
    };
    const pop = (): [number, number] => {
      const c = hc[0] as number;
      const st = hs[0] as number;
      const lc = hc.pop() as number;
      const ls = hs.pop() as number;
      if (hc.length > 0) {
        hc[0] = lc;
        hs[0] = ls;
        let k = 0;
        const n = hc.length;
        for (;;) {
          const l = 2 * k + 1;
          const r = l + 1;
          let m = k;
          if (l < n && (hc[l] as number) < (hc[m] as number)) m = l;
          if (r < n && (hc[r] as number) < (hc[m] as number)) m = r;
          if (m === k) break;
          [hc[m], hc[k]] = [hc[k] as number, hc[m] as number];
          [hs[m], hs[k]] = [hs[k] as number, hs[m] as number];
          k = m;
        }
      }
      return [c, st];
    };
    const cyq = Math.round((node.y - y0) / step);
    const cxq = Math.round((node.x - x0) / step);
    const starts: [number, number, number][] = [
      [hi(b[2] + 8, x0) + 1, cyq, 0],
      [lo(b[0] - 8, x0) - 1, cyq, 1],
      [cxq, hi(b[3] + 8, y0) + 1, 2],
      [cxq, lo(b[1] - 8, y0) - 1, 3],
    ];
    for (const [sx, sy, sd] of starts) {
      if (sx < 0 || sy < 0 || sx >= gw || sy >= gh || blk[sy * gw + sx]) continue;
      const st = (sy * gw + sx) * 4 + sd;
      dist[st] = 0;
      push(0, st);
    }
    let found = -1;
    let pops = 0;
    while (hc.length > 0 && pops++ < 400000) {
      const [c, st] = pop();
      if (c > (dist[st] as number)) continue;
      if (goal.has(st)) {
        found = st;
        break;
      }
      const dir = st & 3;
      const cell = st >> 2;
      const cx = cell % gw;
      const cy = (cell - cx) / gw;
      for (let nd = 0; nd < 4; nd++) {
        if (nd === (dir ^ 1)) continue;
        const nx = cx + (DX[nd] as number);
        const ny = cy + (DY[nd] as number);
        if (nx < 0 || ny < 0 || nx >= gw || ny >= gh) continue;
        const nc = ny * gw + nx;
        if (blk[nc]) continue;
        let cost = c + 1 + (nd !== dir ? 6 : 0) + (nr[nc] ? 2 : 0);
        if (nd < 2) {
          if (vU[nc]) cost += 14;
          if (hU[nc]) cost += 6;
        } else {
          if (hU[nc]) cost += 14;
          if (vU[nc]) cost += 6;
        }
        const ns = nc * 4 + nd;
        const g = goal.get(ns);
        if (g !== undefined) cost += (slotUse[g] as number) * 25;
        if (cost < (dist[ns] as number)) {
          dist[ns] = cost;
          prev[ns] = st;
          push(cost, ns);
        }
      }
    }
    if (found < 0) {
      out[node.id] = null;
      continue;
    }
    const pts: { x: number; y: number; dir: number; c: number }[] = [];
    for (let st = found; st !== -1; st = prev[st] as number) {
      const cl = st >> 2;
      pts.push({ x: x0 + (cl % gw) * step, y: y0 + Math.floor(cl / gw) * step, dir: st & 3, c: cl });
    }
    pts.reverse();
    if (pts.length < 2) {
      // the way out of the node is already in front of the panel: nothing to route, the caller draws the tracks
      out[node.id] = null;
      continue;
    }
    // a cell is used along the direction the link leaves it and the direction it arrived with
    for (let k = 0; k < pts.length; k++) {
      const p = pts[k] as { dir: number; c: number };
      (p.dir < 2 ? hU : vU)[p.c] = 1;
      if (k > 0) (p.dir < 2 ? hU : vU)[(pts[k - 1] as { c: number }).c] = 1;
    }
    const gs = goal.get(found) as number;
    slotUse[gs] = (slotUse[gs] as number) + 1;
    // keep the corners only
    const poly = [pts[0] as (typeof pts)[number]];
    for (let m = 1; m < pts.length - 1; m++) {
      if ((pts[m + 1] as { dir: number }).dir !== (pts[m] as { dir: number }).dir) poly.push(pts[m] as (typeof pts)[number]);
    }
    poly.push(pts[pts.length - 1] as (typeof pts)[number]);
    // The first run keeps the node's own row or column and the last one the slot's, so nothing jogs at the ends.
    // The first move may turn at once (then the leg out of the node is a short run of its own).
    const sl = slot[gs] as { edge: number; at: number };
    const a0 = poly[0] as (typeof pts)[number];
    const firstHoriz = (pts[1] as { dir: number }).dir < 2;
    const exitHoriz = a0.dir < 2;
    if (firstHoriz === exitHoriz) {
      const first = poly[1] as (typeof pts)[number];
      if (exitHoriz) {
        a0.y = node.y;
        first.y = node.y;
      } else {
        a0.x = node.x;
        first.x = node.x;
      }
    } else {
      poly[0] = exitHoriz ? { ...a0, y: node.y } : { ...a0, x: node.x };
    }
    const last = poly[poly.length - 1] as (typeof pts)[number];
    const z = poly[poly.length - 2] as (typeof pts)[number];
    // one straight run is already on the node's line; otherwise the last run is put on the slot's line
    if (!(poly.length === 2 && firstHoriz === exitHoriz)) {
      if (gs < 10) {
        z.y = sl.at;
        last.y = sl.at;
      } else {
        z.x = sl.at;
        last.x = sl.at;
      }
    }
    const end = gs < 10 ? { x: sl.edge, y: last.y } : { x: last.x, y: sl.edge };
    let d = `M ${node.x} ${node.y}`;
    let px = node.x;
    let py = node.y;
    for (const p of [...poly, end]) {
      if (p.x === px && p.y === py) continue;
      d += ` L ${p.x} ${p.y}`;
      px = p.x;
      py = p.y;
    }
    out[node.id] = d;
  }
  return out;
}
