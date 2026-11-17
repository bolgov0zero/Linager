// Run with: node --test src/lib/dashboard/schemeLayout.test.mjs (Node 22.18 or newer strips the types by itself).
import test from 'node:test';
import assert from 'node:assert/strict';
import { autoLayout, overlaps, fitView, zoomAround, speedLevel, FULL_MAX, MIN_ZOOM, MAX_ZOOM } from './schemeLayout.ts';

test('no two nodes touch, for any number of servers', () => {
  for (const n of [0, 1, 2, 3, 7, 10, FULL_MAX, FULL_MAX + 1, 24, 60, 150, 400]) {
    const l = autoLayout(n);
    assert.equal(l.pos.length, n, `a place for each of ${n}`);
    assert.ok(!overlaps(l.pos, l.nw, l.nh, 0), `${n} nodes overlap`);
  }
});

test('nodes keep their size: a large fleet is laid out on a larger plane, in the compact form', () => {
  const small = autoLayout(10);
  const big = autoLayout(150);
  assert.equal(small.mode, 'full');
  assert.equal(big.mode, 'compact');
  const reach = (l) => Math.max(...l.pos.map((p) => Math.abs(p.x)));
  assert.ok(reach(big) > reach(small), 'the plane grows with the number of servers');
});

test('the view fits everything and never zooms in beyond 100%', () => {
  const v = fitView([-1000, -500, 1000, 500], 800, 440);
  assert.ok(v.k < 1 && v.k >= MIN_ZOOM);
  // the middle of the box is the middle of the screen
  assert.ok(Math.abs(v.tx - 400) < 1e-6 && Math.abs(v.ty - 220) < 1e-6);
  assert.equal(fitView([-50, -20, 50, 20], 800, 440).k, 1);
});

test('zooming around a point keeps that point still and stays within the limits', () => {
  const v = { k: 1, tx: 10, ty: 20 };
  const z = zoomAround(v, 300, 200, 2);
  assert.equal(z.k, 2);
  assert.ok(Math.abs(300 - z.tx - (300 - v.tx) * 2) < 1e-9);
  assert.ok(Math.abs(200 - z.ty - (200 - v.ty) * 2) < 1e-9);
  assert.equal(zoomAround(v, 0, 0, 1e6).k, MAX_ZOOM);
  assert.equal(zoomAround(v, 0, 0, 1e-6).k, MIN_ZOOM);
});

test('three speeds of the squares by the sum of the rates', () => {
  assert.equal(speedLevel(0), 1);
  assert.equal(speedLevel(49e3), 1);
  assert.equal(speedLevel(50e3), 2);
  assert.equal(speedLevel(1.9e6), 2);
  assert.equal(speedLevel(2e6), 3);
});

// ---- links with right-angle bends ----
import { routeLinks, busPath, PANEL_W, PANEL_H, ROUTE_MAX } from './schemeLayout.ts';

function segments(d) {
  const p = d.match(/-?\d+(\.\d+)?(e[-+]?\d+)?/gi).map(Number);
  const pts = [];
  for (let i = 0; i < p.length; i += 2) pts.push([p[i], p[i + 1]]);
  return pts;
}

test('right-angle links: every link is axis-aligned and none crosses another node', () => {
  for (const n of [3, 10, ROUTE_MAX]) {
    const l = autoLayout(n);
    const nodes = l.pos.map((p, i) => ({ id: 's' + i, x: p.x, y: p.y }));
    const bounds = [Math.min(...nodes.map((q) => q.x)) - l.nw, Math.min(...nodes.map((q) => q.y)) - l.nh, Math.max(...nodes.map((q) => q.x)) + l.nw, Math.max(...nodes.map((q) => q.y)) + l.nh];
    const routed = routeLinks(nodes, l.nw, l.nh, { x: 0, y: 0 }, bounds);
    let found = 0;
    for (const q of nodes) {
      const d = routed[q.id];
      if (d === null) continue; // allowed: the caller falls back to the tracks
      found++;
      const pts = segments(d);
      for (let k = 0; k + 1 < pts.length; k++) {
        const [ax, ay] = pts[k];
        const [bx, by] = pts[k + 1];
        assert.ok(ax === bx || ay === by, `${n}: a diagonal segment in ${d}`);
        for (const o of nodes) {
          if (o === q) continue;
          const inside = Math.max(ax, bx) > o.x - l.nw / 2 && Math.min(ax, bx) < o.x + l.nw / 2 && Math.max(ay, by) > o.y - l.nh / 2 && Math.min(ay, by) < o.y + l.nh / 2;
          assert.ok(!inside, `${n}: the link of ${q.id} runs through ${o.id}`);
        }
      }
    }
    assert.ok(found >= nodes.length - 1, `${n}: at most one link may fall back, got ${nodes.length - found}`);
  }
});

test('the router falls back instead of failing: no nodes, one node, and a node on the panel', () => {
  assert.deepEqual(routeLinks([], 160, 58, { x: 0, y: 0 }, [0, 0, 1, 1]), {});
  const one = routeLinks([{ id: 'a', x: 400, y: 0 }], 160, 58, { x: 0, y: 0 }, [-200, -100, 500, 100]);
  assert.ok(typeof one.a === 'string' || one.a === null);
  const jammed = routeLinks([{ id: 'a', x: 0, y: 0 }], 160, 58, { x: 0, y: 0 }, [-200, -100, 200, 100]);
  assert.ok(jammed.a === null || typeof jammed.a === 'string');
});

test('the bus path is axis-aligned, starts at the node and ends in the panel row or column', () => {
  for (let i = 0; i < 20; i++) {
    for (const a of [{ x: -400, y: -50 }, { x: 380, y: 120 }, { x: 20, y: -300 }, { x: -30, y: 260 }]) {
      const pts = segments(busPath(a, { x: 0, y: 0 }, i));
      assert.deepEqual(pts[0], [a.x, a.y]);
      for (let k = 0; k + 1 < pts.length; k++) assert.ok(pts[k][0] === pts[k + 1][0] || pts[k][1] === pts[k + 1][1]);
      const e = pts[pts.length - 1];
      assert.ok(Math.abs(e[0]) <= PANEL_W / 2 + 1e-9 + 100 && Math.abs(e[1]) <= PANEL_H / 2 + 60);
    }
  }
});
