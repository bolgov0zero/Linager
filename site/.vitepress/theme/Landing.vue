<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue';
import { useData, withBase } from 'vitepress';
import dict from './landing-i18n.json';

const { lang } = useData();
const code = computed<'en' | 'ru'>(() => (lang.value.startsWith('ru') ? 'ru' : 'en'));
const t = (k: string): string => (dict[code.value] as Record<string, string>)[k] ?? (dict.en as Record<string, string>)[k] ?? k;
const docsLink = computed(() => withBase(code.value === 'ru' ? '/ru/introduction' : '/introduction'));
const shot = (name: string) => withBase(`/screenshots/${code.value}/${name}.png`);

const CMD = 'curl -fsSL https://raw.githubusercontent.com/bolgov0zero/linager/main/install.sh | sh';
const REPO = 'https://github.com/bolgov0zero/linager';

// ---- pixel icons (8x8 grids)
const ICONS: Record<string, string[]> = {
  dash: ['11101110', '10101010', '11101110', '00000000', '11101110', '10101010', '11101110', '00000000'],
  chart: ['00000001', '00000001', '00010001', '00010101', '01010101', '01010101', '01010101', '11111111'],
  term: ['11111111', '10000001', '10100001', '10010001', '10100001', '10001111', '10000001', '11111111'],
  folder: ['01110000', '10001111', '10000001', '10000001', '10000001', '10000001', '10000001', '11111111'],
  down: ['00011000', '00011000', '00011000', '11011011', '01111110', '00111100', '00011000', '11111111'],
  net: ['00011000', '00011000', '00111100', '11111111', '10011001', '10011001', '11100111', '11100111'],
  dock: ['00000000', '00111000', '00111000', '11111111', '11111111', '11111110', '01111100', '00000000'],
  shield: ['01111110', '11111111', '11100111', '11100111', '11100111', '01100110', '00111100', '00011000'],
  lock: ['00111100', '01000010', '01000010', '11111111', '11111111', '11100111', '11100111', '11111111'],
};
function icon(name: string): string {
  const g = ICONS[name];
  let r = '';
  for (let y = 0; y < 8; y++) for (let x = 0; x < 8; x++) if (g[y][x] === '1') r += `<rect x="${x}" y="${y}" width="1" height="1"/>`;
  return `<svg viewBox="0 0 8 8" aria-hidden="true">${r}</svg>`;
}
const FEATS: [string, string][] = [
  ['dash', 'f1'], ['chart', 'f2'], ['term', 'f3'], ['folder', 'f4'], ['down', 'f5'],
  ['net', 'f6'], ['dock', 'f7'], ['shield', 'f8'], ['lock', 'f9'],
];
const STATS: [number, string][] = [[360, 'st.1'], [5, 'st.2'], [30, 'st.3'], [1, 'st.4']];
const TABS: [string, string, string][] = [
  ['dashboard', 's.t1', 's.c1'], ['terminal', 's.t2', 's.c2'], ['firewall', 's.t3', 's.c3'],
  ['docker', 's.t4', 's.c4'], ['access-restriction', 's.t5', 's.c5'],
];

// ---- live mini dashboard
interface Srv { n: string; c: number; r: number; d: number; k: string }
const servers = reactive<Srv[]>([
  { n: 'web-01', c: 35, r: 52, d: 41, k: '' },
  { n: 'web-02', c: 28, r: 47, d: 38, k: 'w' },
  { n: 'db-01', c: 91, r: 86, d: 72, k: 'c' },
  { n: 'cache-01', c: 12, r: 31, d: 22, k: '' },
]);
const bars = (v: number) => Array.from({ length: 20 }, (_, i) => i < Math.round(v / 5));
const barCls = (v: number) => (v > 85 ? 'crit' : v > 65 ? 'warn' : '');

// ---- state
const root = ref<HTMLElement | null>(null);
const counters = reactive<number[]>(STATS.map(() => 0));
const tab = ref(0);
const typed = ref('');
const copied = ref(false);
const ready = ref(false);
let liveTimer: ReturnType<typeof setInterval> | undefined;
let autoTimer: ReturnType<typeof setInterval> | undefined;
let typeRun = 0;
const observers: IntersectionObserver[] = [];

const reduced = () => typeof matchMedia !== 'undefined' && matchMedia('(prefers-reduced-motion: reduce)').matches;

function copy(): void {
  navigator.clipboard?.writeText(CMD).catch(() => {});
  copied.value = true;
  setTimeout(() => (copied.value = false), 1400);
}

const TERM: [string, string][] = [
  ['b', '$ sudo linager install'],
  ['m', '? Role: [S]erver / [C]lient   S'],
  ['g', '✔ PostgreSQL ready (localhost, scram-sha-256)'],
  ['g', '✔ Keys and certificates created'],
  ['g', '✔ Administrator "admin" created'],
  ['g', '✔ linager-server.service started'],
  ['b', '  Admin panel: http://203.0.113.10:8080'],
];
const esc = (s: string) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;');

function runTyped(): void {
  const my = ++typeRun;
  let line = 0;
  let pos = 0;
  let html = '';
  if (reduced()) {
    typed.value = TERM.map(([c, s]) => `<span class="${c}">${esc(s)}</span>`).join('\n');
    return;
  }
  const step = (): void => {
    if (my !== typeRun) return;
    if (line >= TERM.length) {
      typed.value = html + '<span class="caret"></span>';
      return;
    }
    const [c, s] = TERM[line];
    if (s.startsWith('$')) {
      pos++;
      typed.value = html + `<span class="${c}">${esc(s.slice(0, pos))}</span><span class="caret"></span>`;
      if (pos >= s.length) {
        html += `<span class="${c}">${esc(s)}</span>\n`;
        line++;
        pos = 0;
        setTimeout(step, 500);
      } else setTimeout(step, 32);
    } else {
      html += `<span class="${c}">${esc(s)}</span>\n`;
      typed.value = html + '<span class="caret"></span>';
      line++;
      setTimeout(step, 420);
    }
  };
  step();
}

onMounted(() => {
  ready.value = true;
  const el = root.value!;
  if (!reduced()) liveTimer = setInterval(() => {
    for (const s of servers) {
      s.c = Math.max(3, Math.min(97, s.c + (Math.random() - 0.5) * 14));
      s.r = Math.max(10, Math.min(95, s.r + (Math.random() - 0.5) * 3));
    }
  }, 1400);

  const reveal = new IntersectionObserver((es) => {
    for (const e of es) if (e.isIntersecting) { e.target.classList.add('in'); reveal.unobserve(e.target); }
  }, { threshold: 0.15 });
  el.querySelectorAll('.reveal').forEach((n) => reveal.observe(n));
  observers.push(reveal);

  const count = new IntersectionObserver((es) => {
    for (const e of es) {
      if (!e.isIntersecting) continue;
      count.unobserve(e.target);
      const i = Number((e.target as HTMLElement).dataset.i);
      const to = STATS[i][0];
      if (reduced()) { counters[i] = to; continue; }
      const t0 = performance.now();
      const f = (now: number): void => {
        const p = Math.min(1, (now - t0) / 1100);
        counters[i] = Math.round(to * (1 - Math.pow(1 - p, 3)));
        if (p < 1) requestAnimationFrame(f);
      };
      requestAnimationFrame(f);
    }
  }, { threshold: 0.6 });
  el.querySelectorAll('[data-i]').forEach((n) => count.observe(n));
  observers.push(count);

  const term = new IntersectionObserver((es) => {
    for (const e of es) if (e.isIntersecting) { term.disconnect(); runTyped(); }
  }, { threshold: 0.4 });
  const tp = el.querySelector('.term');
  if (tp) term.observe(tp);
  observers.push(term);

  const stage = new IntersectionObserver((es) => {
    for (const e of es) {
      clearInterval(autoTimer);
      if (e.isIntersecting && !reduced()) autoTimer = setInterval(() => (tab.value = (tab.value + 1) % TABS.length), 4500);
    }
  }, { threshold: 0.4 });
  const st = el.querySelector('.stage');
  if (st) stage.observe(st);
  observers.push(stage);
});

onBeforeUnmount(() => {
  clearInterval(liveTimer);
  clearInterval(autoTimer);
  typeRun++;
  observers.forEach((o) => o.disconnect());
});

function pick(i: number): void {
  clearInterval(autoTimer);
  tab.value = i;
}
</script>

<template>
  <div ref="root" class="lg" :class="{ js: ready }">
    <section class="hero">
      <div class="wrap hero-grid">
        <div>
          <span class="eyebrow">{{ t('hero.eyebrow') }}</span>
          <h1>{{ t('hero.h1') }}<span class="cur"></span></h1>
          <p class="lead">{{ t('hero.lead') }}</p>
          <div class="cta">
            <a class="btn primary" href="#install">{{ t('hero.install') }}</a>
            <a class="btn" :href="docsLink">{{ t('hero.docs') }} →</a>
          </div>
          <div class="cmd">
            <code><b>$</b> {{ CMD }}</code>
            <button type="button" @click="copy">{{ copied ? t('copied') : t('copy') }}</button>
          </div>
          <div class="meta">
            <span><i class="dot ok"></i> {{ t('hero.m1') }}</span>
            <span><i class="dot ok"></i> {{ t('hero.m2') }}</span>
            <span><i class="dot ok"></i> {{ t('hero.m3') }}</span>
          </div>
        </div>
        <div class="live card" aria-hidden="true">
          <div class="bar"><i></i><i></i><i></i><span>{{ t('live.title') }}</span></div>
          <div class="grid">
            <div v-for="s in servers" :key="s.n" class="sc" :class="s.k">
              <h4><i class="dot" :class="s.k === 'c' ? 'crit' : s.k === 'w' ? 'warn' : 'ok'"></i>{{ s.n }}</h4>
              <div class="row"><span>CPU</span><div class="pbar" :class="barCls(s.c)"><i v-for="(on, i) in bars(s.c)" :key="i" :class="{ on }"></i></div><b>{{ Math.round(s.c) }}%</b></div>
              <div class="row"><span>RAM</span><div class="pbar" :class="s.r > 85 ? 'crit' : ''"><i v-for="(on, i) in bars(s.r)" :key="i" :class="{ on }"></i></div><b>{{ Math.round(s.r) }}%</b></div>
              <div class="row"><span>DISK</span><div class="pbar"><i v-for="(on, i) in bars(s.d)" :key="i" :class="{ on }"></i></div><b>{{ Math.round(s.d) }}%</b></div>
            </div>
          </div>
          <div class="toast">{{ t('live.toast') }}</div>
        </div>
      </div>
    </section>

    <section class="stats">
      <div class="wrap stats-grid">
        <div v-for="(s, i) in STATS" :key="i" class="stat"><b :data-i="i">{{ counters[i] }}</b><span>{{ t(s[1]) }}</span></div>
      </div>
    </section>

    <section class="sec">
      <div class="wrap">
        <span class="eyebrow">{{ t('f.eyebrow') }}</span>
        <h2>{{ t('f.h2') }}</h2>
        <p class="lead">{{ t('f.lead') }}</p>
        <div class="fgrid">
          <div v-for="(f, i) in FEATS" :key="f[1]" class="card feat reveal" :style="{ '--d': (i % 3) * 0.08 + 's' }">
            <div class="ic" v-html="icon(f[0])"></div>
            <h3>{{ t(f[1] + '.t') }} <span v-if="f[1] === 'f9'" class="new">{{ t('new') }}</span></h3>
            <p>{{ t(f[1] + '.p') }}</p>
          </div>
        </div>
      </div>
    </section>

    <section class="sec how">
      <div class="wrap">
        <span class="eyebrow">{{ t('h.eyebrow') }}</span>
        <h2>{{ t('h.h2') }}</h2>
        <p class="lead">{{ t('h.lead') }}</p>
        <div class="card diagram reveal">
          <svg viewBox="0 0 900 300" role="img" aria-label="architecture">
            <g><rect class="box" x="20" y="110" width="130" height="70" /><text class="tt" x="85" y="142" text-anchor="middle">{{ t('h.browser') }}</text><text x="85" y="162" text-anchor="middle" fill="var(--muted)">HTTPS</text></g>
            <g><rect class="box" x="215" y="110" width="150" height="70" stroke-dasharray="6 4" /><text class="tt" x="290" y="142" text-anchor="middle">{{ t('h.proxy') }}</text><text x="290" y="162" text-anchor="middle" fill="var(--muted)">{{ t('h.proxy2') }}</text></g>
            <g>
              <rect class="box dark" x="430" y="60" width="190" height="170" />
              <text class="tt inv" x="525" y="105" text-anchor="middle">{{ t('h.server') }}</text>
              <text class="sub" x="525" y="125" text-anchor="middle">{{ t('h.server2') }}</text>
              <rect class="dbbox" x="450" y="150" width="150" height="62" />
              <text class="acc" x="525" y="176" text-anchor="middle">{{ t('h.db') }}</text>
              <text class="sub" x="525" y="196" text-anchor="middle">{{ t('h.db2') }}</text>
            </g>
            <g v-for="(a, i) in [['h.a1', 40], ['h.a2', 130], ['h.a3', 220]]" :key="i"><rect class="box" x="720" :y="a[1]" width="150" height="56" /><text class="tt" x="795" :y="(a[1] as number) + 34" text-anchor="middle">{{ t(a[0] as string) }}</text></g>
            <path class="ln" d="M150 145 H215" /><path class="ln" d="M365 145 H430" />
            <path class="ln" d="M720 68 H680 V110 H620" /><path class="ln" d="M720 158 H620" /><path class="ln" d="M720 248 H680 V190 H620" />
            <rect class="pk" width="8" height="8" x="-4" y="-4"><animateMotion dur="3s" repeatCount="indefinite" path="M150 145 H430" /></rect>
            <rect class="pk" width="8" height="8" x="-4" y="-4"><animateMotion dur="2.4s" repeatCount="indefinite" path="M720 68 H680 V110 H620" /></rect>
            <rect class="pk" width="8" height="8" x="-4" y="-4"><animateMotion dur="2.8s" begin="-1s" repeatCount="indefinite" path="M720 158 H620" /></rect>
            <rect class="pk" width="8" height="8" x="-4" y="-4"><animateMotion dur="3.2s" begin="-2s" repeatCount="indefinite" path="M720 248 H680 V190 H620" /></rect>
            <text x="520" y="285" text-anchor="middle" fill="var(--muted)">{{ t('h.l2') }}</text>
          </svg>
        </div>
        <div class="points">
          <div class="reveal"><b>{{ t('h.p1b') }}</b>{{ t('h.p1') }}</div>
          <div class="reveal" style="--d: 0.1s"><b>{{ t('h.p2b') }}</b>{{ t('h.p2') }}</div>
          <div class="reveal" style="--d: 0.2s"><b>{{ t('h.p3b') }}</b>{{ t('h.p3') }}</div>
        </div>
      </div>
    </section>

    <section class="sec">
      <div class="wrap">
        <span class="eyebrow">{{ t('s.eyebrow') }}</span>
        <h2>{{ t('s.h2') }}</h2>
        <p class="lead">{{ t('s.lead') }}</p>
        <div class="tabs" role="tablist">
          <button v-for="(tb, i) in TABS" :key="tb[0]" type="button" role="tab" :aria-selected="tab === i" @click="pick(i)">{{ t(tb[1]) }}</button>
        </div>
        <div class="card shot reveal">
          <div class="chrome"><i></i><i></i><i></i><span>linager.example.com</span></div>
          <div class="stage"><img v-for="(tb, i) in TABS" :key="tb[0]" :class="{ on: tab === i }" :src="shot(tb[0])" alt="" loading="lazy" /></div>
        </div>
        <p class="cap">{{ t(TABS[tab][2]) }}</p>
      </div>
    </section>

    <section class="sec" style="padding-top: 20px">
      <div class="wrap spot">
        <div>
          <span class="eyebrow">{{ t('a.eyebrow') }}</span>
          <h2>{{ t('a.h2') }}</h2>
          <p class="lead">{{ t('a.lead') }}</p>
          <ul class="checks">
            <li v-for="n in 5" :key="n" class="reveal" :style="{ '--d': n * 0.07 + 's' }">{{ t('a.' + n) }}</li>
          </ul>
        </div>
        <div class="card reveal"><img alt="" style="display: block; width: 100%" :src="shot('access-journal')" loading="lazy" /></div>
      </div>
    </section>

    <section id="install" class="sec how">
      <div class="wrap">
        <span class="eyebrow">{{ t('i.eyebrow') }}</span>
        <h2>{{ t('i.h2') }}</h2>
        <p class="lead">{{ t('i.lead') }}</p>
        <div class="steps">
          <div v-for="n in 3" :key="n" class="card step reveal" :style="{ '--d': (n - 1) * 0.1 + 's' }">
            <h3>{{ t('i.' + n + 't') }}</h3>
            <p>{{ t('i.' + n + 'p') }}</p>
          </div>
        </div>
        <div class="term reveal"><div class="tb"><i></i><i></i><i></i></div><pre v-html="typed"></pre></div>
        <div class="platforms"><span>Debian 12</span><span>Debian 13</span><span>Ubuntu 22.04</span><span>Ubuntu 24.04</span><span>Ubuntu 26.04</span><span>amd64</span><span>arm64 (agent)</span></div>
      </div>
    </section>

    <section class="cta-band">
      <div class="wrap">
        <h2>{{ t('c.h2') }}</h2>
        <p>{{ t('c.p') }}</p>
        <div class="cta-row">
          <a class="btn" href="#install">{{ t('hero.install') }}</a>
          <a class="btn" :href="docsLink">{{ t('hero.docs') }} →</a>
          <a class="btn" :href="REPO" target="_blank" rel="noopener">GitHub ↗</a>
        </div>
      </div>
    </section>
  </div>
</template>

<style>
.lg{
  --bg:var(--lg-bg);--surface:var(--lg-surface);--ink:var(--lg-ink);--muted:var(--lg-muted);--line:var(--lg-ink);--accent:var(--lg-accent);
  --good:var(--lg-good);--warn:var(--lg-warn);--crit:var(--lg-crit);--seg-off:var(--lg-off);--shade:var(--lg-shade);--term:var(--lg-term);--term-ink:#e6e9ef;
  --font-display:var(--lg-display);
  font-size:15px;line-height:1.6;color:var(--ink);background:var(--bg);
}
.dark .lg{--line:#3b4455}
.lg *{box-sizing:border-box}
.lg h1,.lg h2,.lg h3,.lg h4,.lg p,.lg ul{margin:0;border:0;padding:0}
.lg a{text-decoration:none}
.lg .wrap{max-width:1180px;margin:0 auto;padding-left:max(16px,env(safe-area-inset-left,0px));padding-right:max(16px,env(safe-area-inset-right,0px))}
.lg .muted{color:var(--muted)}
.lg .card{background:var(--surface);border:2px solid var(--line);box-shadow:4px 4px 0 var(--shade)}
.lg .eyebrow{font-family:var(--font-display);font-size:13px;letter-spacing:.14em;text-transform:uppercase;color:var(--accent)}
.lg .sec{padding:84px 0}
.lg .sec h2{font-family:var(--font-display);font-weight:400;font-size:clamp(28px,4vw,44px);line-height:1.1;margin:8px 0 14px;letter-spacing:0}
.lg .sec .lead{max-width:640px;color:var(--muted);font-size:16px}
.lg .dot{display:inline-block;width:8px;height:8px;background:currentColor}
.lg .ok{color:var(--good)}.lg .warn{color:var(--warn)}.lg .crit{color:var(--crit)}
.lg.js .reveal{opacity:0;transform:translateY(22px);transition:opacity .6s ease,transform .6s ease;transition-delay:var(--d,0s)}
.lg.js .reveal.in{opacity:1;transform:none}
.lg .btn{display:inline-flex;align-items:center;justify-content:center;gap:8px;height:40px;padding:0 16px;background:var(--surface);color:var(--ink);border:2px solid var(--line);border-radius:0;cursor:pointer;font-family:var(--font-display);font-size:16px;line-height:1;letter-spacing:.02em;box-shadow:3px 3px 0 var(--shade);white-space:nowrap;transition:transform .08s,box-shadow .08s}
.lg .btn:hover{transform:translate(-1px,-1px);box-shadow:5px 5px 0 var(--shade)}
.lg .btn:active{transform:translate(2px,2px);box-shadow:1px 1px 0 var(--shade)}
.lg .btn.primary{background:var(--accent);color:var(--lg-surface);border-color:var(--accent)}
.dark .lg .btn.primary{color:#0f1218}

.lg .pbar{display:grid;grid-template-columns:repeat(20,1fr);gap:2px;height:10px}
.lg .pbar i{background:var(--seg-off);transition:background .25s}
.lg .pbar i.on{background:var(--accent)}
.lg .pbar.warn i.on{background:var(--warn)}
.lg .pbar.crit i.on{background:var(--crit)}

/* hero */
.lg .hero{position:relative;overflow:hidden;padding:72px 0 80px;border-bottom:2px solid var(--line)}
.lg .hero::before{content:"";position:absolute;inset:0;background-image:linear-gradient(var(--seg-off) 1px,transparent 1px),linear-gradient(90deg,var(--seg-off) 1px,transparent 1px);background-size:28px 28px;opacity:.55;mask-image:radial-gradient(ellipse at 70% 40%,#000 0,transparent 70%);-webkit-mask-image:radial-gradient(ellipse at 70% 40%,#000 0,transparent 70%);pointer-events:none}
.lg .hero-grid{position:relative;display:grid;grid-template-columns:1.05fr .95fr;gap:48px;align-items:center}
.lg .hero h1{font-family:var(--font-display);font-weight:400;font-size:clamp(38px,6vw,72px);line-height:1.02;letter-spacing:-.01em;margin:14px 0 20px}
.lg .hero h1 .cur{display:inline-block;width:.5em;height:.8em;background:var(--accent);margin-left:6px;vertical-align:-.05em;animation:lgblink 1s steps(1) infinite}
@keyframes lgblink{50%{opacity:0}}
.lg .hero p.lead{font-size:17px;color:var(--muted);max-width:520px}
.lg .cta{display:flex;flex-wrap:wrap;gap:12px;margin:28px 0 22px}
.lg .cmd{display:flex;max-width:540px;background:var(--term);color:var(--term-ink);border:2px solid var(--line);box-shadow:4px 4px 0 var(--shade)}
.lg .cmd code{flex:1;min-width:0;padding:10px 14px;font-size:12px;white-space:pre-wrap;word-break:break-all;background:none;color:inherit;border:0}
.lg .cmd code b{color:#7d9cff;font-weight:400}
.lg .cmd button{align-self:stretch;border:0;border-left:2px solid var(--line);background:var(--surface);color:var(--ink);padding:0 14px;cursor:pointer;font-family:var(--font-display);font-size:14px}
.lg .cmd button:hover{background:var(--accent);color:var(--lg-surface)}
.lg .meta{display:flex;flex-wrap:wrap;gap:8px 18px;margin-top:20px;font-size:12px;color:var(--muted)}
.lg .live{position:relative;transform:rotate(-1.2deg);animation:lgfloat 7s ease-in-out infinite}
@keyframes lgfloat{50%{transform:rotate(-1.2deg) translateY(-8px)}}
.lg .live .bar{display:flex;align-items:center;gap:6px;padding:8px 12px;border-bottom:2px solid var(--line);font-size:12px;color:var(--muted)}
.lg .live .bar i{width:10px;height:10px;border:2px solid var(--line);background:var(--seg-off)}
.lg .live .bar span{margin-left:8px}
.lg .live .grid{display:grid;grid-template-columns:1fr 1fr;gap:12px;padding:14px}
.lg .sc{border:2px solid var(--line);border-top-width:6px;padding:10px 12px;background:var(--surface)}
.lg .sc.w{border-top-color:var(--warn)}.lg .sc.c{border-top-color:var(--crit)}
.lg .sc h4{font-family:var(--font-display);font-weight:400;font-size:18px;display:flex;align-items:center;gap:8px}
.lg .sc .row{display:grid;grid-template-columns:34px 1fr 38px;gap:8px;align-items:center;font-size:11px;color:var(--muted);margin-top:6px}
.lg .sc .row b{font-weight:400;color:var(--ink);text-align:right}
.lg .live .toast{position:absolute;right:-14px;bottom:-20px;background:var(--ink);color:var(--surface);padding:10px 14px;font-size:12px;box-shadow:4px 4px 0 var(--shade);animation:lgpop 6s ease-in-out infinite}
@keyframes lgpop{0%,8%{opacity:0;transform:translateY(10px)}14%,80%{opacity:1;transform:none}90%,100%{opacity:0;transform:translateY(10px)}}

/* stats */
.lg .stats{background:#14181e;color:#f6f7f3}
.dark .lg .stats{background:#0a0c10}
.lg .stats-grid{display:grid;grid-template-columns:repeat(4,1fr)}
.lg .stat{padding:30px 20px;border-left:2px solid #3b4455}
.lg .stat:first-child{border-left:0}
.lg .stat b{display:block;font-family:var(--font-display);font-weight:400;font-size:clamp(34px,4.4vw,52px);line-height:1}
.lg .stat span{display:block;margin-top:8px;font-size:13px;color:#aab2c2}

/* features */
.lg .fgrid{display:grid;grid-template-columns:repeat(3,1fr);gap:20px;margin-top:40px}
.lg .feat{padding:20px;transition:transform .15s,box-shadow .15s,opacity .6s}
.lg .feat:hover{transform:translate(-2px,-2px);box-shadow:7px 7px 0 var(--shade)}
.lg .feat .ic{width:44px;height:44px;border:2px solid var(--line);display:grid;place-items:center;background:var(--bg);margin-bottom:14px}
.lg .feat .ic svg{width:26px;height:26px;shape-rendering:crispEdges;fill:var(--ink)}
.lg .feat:hover .ic{background:var(--accent)}
.lg .feat:hover .ic svg{fill:var(--surface)}
.lg .feat h3{font-family:var(--font-display);font-weight:400;font-size:20px;margin-bottom:6px;display:flex;gap:8px;align-items:center}
.lg .feat p{color:var(--muted);font-size:14px}
.lg .new{padding:0 6px;border:2px solid var(--accent);color:var(--accent);font-size:11px;letter-spacing:.04em;text-transform:uppercase;font-family:var(--lg-display)}

/* how */
.lg .how{background:var(--surface);border-block:2px solid var(--line)}
.lg .diagram{margin-top:40px;padding:22px;background:var(--bg)}
.lg .diagram svg{display:block;width:100%;height:auto}
.lg .diagram text{font-family:"Medeo Mono",monospace;font-size:12px;fill:var(--ink)}
.lg .diagram .tt{font-family:var(--font-display);font-size:16px}
.lg .diagram .box{fill:var(--surface);stroke:var(--line);stroke-width:2}
.lg .diagram .box.dark{fill:var(--ink);stroke:var(--ink)}
.lg .diagram .inv{fill:var(--surface)}
.lg .diagram .sub{fill:#aab2c2}
.lg .diagram .acc{fill:#7d9cff}
.lg .diagram .dbbox{fill:none;stroke:#7d9cff;stroke-width:2}
.lg .diagram .ln{stroke:var(--line);stroke-width:2;stroke-dasharray:6 6;fill:none;animation:lgdash 1.4s linear infinite}
@keyframes lgdash{to{stroke-dashoffset:-24}}
.lg .diagram .pk{fill:var(--accent)}
.lg .points{display:grid;grid-template-columns:repeat(3,1fr);gap:20px;margin-top:26px}
.lg .points div{border-left:4px solid var(--accent);padding-left:14px;font-size:14px;color:var(--muted)}
.lg .points b{display:block;color:var(--ink);font-family:var(--font-display);font-weight:400;font-size:17px;margin-bottom:2px}

/* screenshots */
.lg .tabs{display:flex;flex-wrap:wrap;gap:8px;margin:32px 0 18px}
.lg .tabs button{height:36px;padding:0 14px;border:2px solid var(--line);background:var(--surface);color:var(--ink);cursor:pointer;font-family:var(--font-display);font-size:15px;box-shadow:3px 3px 0 var(--shade)}
.lg .tabs button[aria-selected=true]{background:var(--ink);color:var(--surface)}
.lg .shot{position:relative;overflow:hidden}
.lg .shot .chrome{display:flex;gap:6px;align-items:center;padding:8px 12px;border-bottom:2px solid var(--line);font-size:12px;color:var(--muted)}
.lg .shot .chrome i{width:10px;height:10px;border:2px solid var(--line)}
.lg .shot .stage{position:relative;aspect-ratio:1600/1130;background:var(--bg)}
.lg .shot img{position:absolute;inset:0;width:100%;height:100%;object-fit:cover;object-position:top;opacity:0;transform:scale(1.015);transition:opacity .5s,transform .8s;border:0;box-shadow:none}
.lg .shot img.on{opacity:1;transform:none}
.lg .cap{margin-top:14px;color:var(--muted);font-size:14px;min-height:46px}

/* spotlight */
.lg .spot{display:grid;grid-template-columns:.9fr 1.1fr;gap:44px;align-items:center}
.lg .checks{list-style:none;padding:0;margin:20px 0 0;display:grid;gap:10px}
.lg .checks li{display:flex;gap:10px;align-items:flex-start;font-size:14px}
.lg .checks li::before{content:"";flex:none;width:14px;height:14px;margin-top:5px;background:var(--accent);clip-path:polygon(0 55%,20% 40%,40% 62%,85% 10%,100% 25%,40% 90%)}

/* install */
.lg .steps{display:grid;grid-template-columns:repeat(3,1fr);gap:20px;margin-top:40px;counter-reset:s}
.lg .step{padding:22px;position:relative}
.lg .step::before{counter-increment:s;content:"0" counter(s);position:absolute;top:-16px;left:16px;background:var(--accent);color:var(--lg-surface);font-family:var(--font-display);font-size:18px;padding:0 10px;border:2px solid var(--line)}
.lg .step h3{font-family:var(--font-display);font-weight:400;font-size:20px;margin:4px 0 8px}
.lg .step p{color:var(--muted);font-size:14px}
.lg .term{margin-top:30px;background:var(--term);color:var(--term-ink);border:2px solid var(--line);box-shadow:4px 4px 0 var(--shade);font-size:13px}
.lg .term .tb{display:flex;gap:6px;padding:8px 12px;border-bottom:2px solid #3b4455}
.lg .term .tb i{width:10px;height:10px;border:2px solid #3b4455}
.lg .term pre{margin:0;padding:16px;min-height:178px;white-space:pre-wrap;font-family:"Medeo Mono",monospace;line-height:1.55;background:none;color:inherit}
.lg .term .g{color:#4fcf86}.lg .term .b{color:#7d9cff}.lg .term .m{color:#8a93a4}
.lg .term .caret{display:inline-block;width:8px;height:15px;background:#7d9cff;vertical-align:-2px;animation:lgblink 1s steps(1) infinite}
.lg .platforms{display:flex;flex-wrap:wrap;gap:8px;margin-top:18px}
.lg .platforms span{border:2px solid var(--line);padding:2px 10px;font-size:13px;background:var(--bg)}

/* call to action */
.lg .cta-band{background:var(--lg-accent);color:#fff;border-block:2px solid var(--line);text-align:center;padding:64px 0}
.dark .lg .cta-band{color:#0f1218}
.lg .cta-band h2{font-family:var(--font-display);font-weight:400;font-size:clamp(28px,4vw,46px);margin-bottom:10px}
.lg .cta-band p{opacity:.9;margin-bottom:24px}
.lg .cta-band .btn{border-color:var(--line);color:var(--ink)}
.lg .cta-row{display:flex;gap:12px;justify-content:center;flex-wrap:wrap}

@media (max-width:900px){
  .lg .hero-grid,.lg .spot{grid-template-columns:1fr}
  .lg .live{max-width:520px}
  .lg .fgrid{grid-template-columns:1fr 1fr}
  .lg .stats-grid{grid-template-columns:1fr 1fr}.lg .stat:nth-child(3){border-left:0}.lg .stat:nth-child(n+3){border-top:2px solid #3b4455}
  .lg .steps,.lg .points{grid-template-columns:1fr}
}
@media (max-width:640px){
  .lg .fgrid{grid-template-columns:1fr}
  .lg .sec{padding:56px 0}
}
@media (prefers-reduced-motion:reduce){
  .lg.js .reveal{opacity:1;transform:none}
}
</style>
