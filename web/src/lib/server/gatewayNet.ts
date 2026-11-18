// Pure helpers of the Gateway tab. The rules about which networks may be scanned mirror internal/gwnet/gwnet.go:
// the server (and the agent) is the authority, this file only gives early feedback while the administrator types.
// No imports on purpose: the file is run by node in gatewayNet.test.mjs.

/** How many networks one gateway may have (gwnet.MaxNets). */
export const MAX_NETS = 16;
/** The shortest prefix that is scanned: a /24 (gwnet.MinBits). */
export const MIN_BITS = 24;
/** The longest prefix: a /30 (gwnet.MaxBits). */
export const MAX_BITS = 30;
/** The shortest prefix that can be split into /24 networks (gwnet.Split24). */
export const SPLIT_MIN_BITS = 16;

export type NetKind = 'private' | 'public';
export type NetErrorCode = 'empty' | 'syntax' | 'ipv6' | 'loopback' | 'reserved' | 'too_small' | 'too_big';

export interface NetOk {
  ok: true;
  /** The network with the host bits cleared. */
  cidr: string;
  bits: number;
  kind: NetKind;
  /** false when the typed address had host bits (the masked network is added instead). */
  aligned: boolean;
  /** Addresses that can hold a device (without the network and the broadcast address). */
  hosts: number;
}

export interface NetError {
  ok: false;
  error: NetErrorCode;
  /** too_big: the prefix length that was typed. */
  bits?: number;
  /** too_big: the typed network with the host bits cleared (the one to split). */
  cidr?: string;
  /** too_big: into how many /24 networks it splits; 0 when it is not offered (too large, or it does not fit in the limit). */
  split?: number;
  /** too_big: the kind of the typed range, for the public network warning. */
  kind?: NetKind;
}

export type ParsedNet = NetOk | NetError;

const OCTET = /^(0|[1-9]\d{0,2})$/;

function parseV4(s: string): number | null {
  const parts = s.split('.');
  if (parts.length !== 4) return null;
  let n = 0;
  for (const p of parts) {
    if (!OCTET.test(p)) return null;
    const v = Number(p);
    if (v > 255) return null;
    n = n * 256 + v;
  }
  return n;
}

function formatV4(n: number): string {
  return [Math.floor(n / 16777216) % 256, Math.floor(n / 65536) % 256, Math.floor(n / 256) % 256, n % 256].join('.');
}

function maskOf(bits: number): number {
  return bits === 0 ? 0 : (0xffffffff << (32 - bits)) >>> 0;
}

/** Private ranges: 10/8, 172.16/12, 192.168/16 and the carrier-grade 100.64/10 (gwnet.Classify). */
export function classifyAddr(a: number): NetKind {
  const o1 = Math.floor(a / 16777216) % 256;
  const o2 = Math.floor(a / 65536) % 256;
  if (o1 === 10) return 'private';
  if (o1 === 172 && o2 >= 16 && o2 <= 31) return 'private';
  if (o1 === 192 && o2 === 168) return 'private';
  if (o1 === 100 && o2 >= 64 && o2 <= 127) return 'private';
  return 'public';
}

/** How many addresses of a network can hold a device: 254 for a /24, 2^(32-bits)-2 in general. */
export function hostCount(bits: number): number {
  return Math.max(0, 2 ** (32 - bits) - 2);
}

/** How many /24 networks a /16../23 splits into; 0 for any other prefix (gwnet.Split24). */
export function splitCount(bits: number): number {
  return bits >= SPLIT_MIN_BITS && bits < MIN_BITS ? 2 ** (MIN_BITS - bits) : 0;
}

/**
 * Checks a network typed by the administrator.
 * `free` is how many more networks the gateway may take; a /16../23 is offered to be split only when its /24 parts fit.
 */
export function parseNet(text: string, free: number = MAX_NETS): ParsedNet {
  const s = text.trim();
  if (s === '') return { ok: false, error: 'empty' };
  const slash = s.indexOf('/');
  if (slash < 0) return { ok: false, error: 'syntax' };
  const addrText = s.slice(0, slash);
  const bitsText = s.slice(slash + 1);
  // netip.ParsePrefix: digits only, no sign and no leading zero
  if (!/^(0|[1-9]\d{0,2})$/.test(bitsText)) return { ok: false, error: 'syntax' };
  let bits = Number(bitsText);

  let addr: number | null;
  if (addrText.includes(':')) {
    // IPv6 is refused; an IPv4-mapped network is counted in IPv6 bits (::ffff:10.0.0.0/120 is 10.0.0.0/24)
    const mapped = /^::ffff:(\d+\.\d+\.\d+\.\d+)$/i.exec(addrText);
    addr = mapped ? parseV4(mapped[1]) : null;
    if (mapped && addr !== null) {
      if (bits > 128) return { ok: false, error: 'syntax' };
      if (bits < 96) return { ok: false, error: 'ipv6' };
      bits -= 96;
    } else if (/^[0-9a-f:.]+$/i.test(addrText) && addrText.split(':').length >= 3) {
      return { ok: false, error: bits > 128 ? 'syntax' : 'ipv6' };
    } else {
      return { ok: false, error: 'syntax' };
    }
  } else {
    addr = parseV4(addrText);
    if (addr === null || bits > 32) return { ok: false, error: 'syntax' };
  }
  if (addr === null) return { ok: false, error: 'syntax' };

  const o1 = Math.floor(addr / 16777216) % 256;
  const o2 = Math.floor(addr / 65536) % 256;
  if (o1 === 127) return { ok: false, error: 'loopback' };
  // multicast 224/4, link-local 169.254/16, 0.0.0.0 and 240/4 (reserved)
  if (o1 >= 224 || (o1 === 169 && o2 === 254) || addr === 0) return { ok: false, error: 'reserved' };
  if (bits > MAX_BITS) return { ok: false, error: 'too_small' };

  const base = (addr & maskOf(bits)) >>> 0;
  const kind = classifyAddr(base);
  if (bits < MIN_BITS) {
    const parts = splitCount(bits);
    return { ok: false, error: 'too_big', bits, cidr: `${formatV4(base)}/${bits}`, split: parts > 0 && parts <= free ? parts : 0, kind };
  }
  return { ok: true, cidr: `${formatV4(base)}/${bits}`, bits, kind, aligned: base === addr, hosts: hostCount(bits) };
}

/** Cuts a network of /16 to /23 into /24 networks; any other network gives an empty list (gwnet.Split24). */
export function split24(cidr: string): string[] {
  const m = /^(\d+\.\d+\.\d+\.\d+)\/(\d+)$/.exec(cidr.trim());
  if (!m) return [];
  const addr = parseV4(m[1]);
  const bits = Number(m[2]);
  if (addr === null) return [];
  const n = splitCount(bits);
  if (n === 0) return [];
  const base = (addr & maskOf(bits)) >>> 0;
  const out: string[] = [];
  for (let i = 0; i < n; i++) out.push(`${formatV4(base + i * 256)}/${MIN_BITS}`);
  return out;
}

/** The first and the last address of a masked network as numbers, or null for a text that is not a network. */
function range(cidr: string): [number, number] | null {
  const m = /^(\d+\.\d+\.\d+\.\d+)\/(\d+)$/.exec(cidr);
  if (!m) return null;
  const addr = parseV4(m[1]);
  const bits = Number(m[2]);
  if (addr === null || bits > 32) return null;
  const first = (addr & maskOf(bits)) >>> 0;
  return [first, first + 2 ** (32 - bits) - 1];
}

/** Whether two networks share addresses (one contains the other). */
export function overlaps(a: string, b: string): boolean {
  const x = range(a);
  const y = range(b);
  return x !== null && y !== null && x[0] <= y[1] && y[0] <= x[1];
}

/** The network of the list that clashes with `cidr`: the same one (`same`) or one that overlaps it. */
export function clash(cidr: string, existing: readonly string[]): { cidr: string; same: boolean } | null {
  for (const e of existing) {
    if (e === cidr) return { cidr: e, same: true };
  }
  for (const e of existing) {
    if (overlaps(cidr, e)) return { cidr: e, same: false };
  }
  return null;
}

/** Compares IPv4 addresses numerically, octet by octet; text that is not an address goes last. */
export function ipCompare(a: string, b: string): number {
  const x = parseV4(a);
  const y = parseV4(b);
  if (x === null && y === null) return a.localeCompare(b);
  if (x === null) return 1;
  if (y === null) return -1;
  return x - y;
}

/** The locale key of a name source (ptr, mdns, netbios, ssdp, own); null for a source that is not known. */
export function nameSrcKey(src: string | undefined): string | null {
  return src && ['ptr', 'mdns', 'netbios', 'ssdp', 'own'].includes(src) ? `gw.src.${src}` : null;
}

/** The short names of the well-known ports shown on the port tags ("22 SSH"); null for a port without a name. */
const PORTS: Record<number, string> = {
  22: 'SSH',
  53: 'DNS',
  80: 'HTTP',
  139: 'NetBIOS',
  443: 'HTTPS',
  445: 'SMB',
  554: 'RTSP',
  1400: 'Sonos',
  1883: 'MQTT',
  3389: 'RDP',
  5000: 'DSM',
  5001: 'DSM',
  8001: 'Samsung TV',
  8006: 'Proxmox',
  8080: 'HTTP',
  8291: 'Winbox',
  9100: 'RAW',
};

export function portLabel(port: number): string | null {
  return PORTS[port] ?? null;
}

/** The text of a port tag: "22 SSH", or just the number when the service is not known. */
export function portTag(port: number): string {
  const name = portLabel(port);
  return name ? `${port} ${name}` : String(port);
}

/** The reason of a device type as the panel sends it (api.ts GwWhy). */
export interface WhyItem {
  kind: string;
  value?: string;
}

/** The kinds of reasons that have a text in the locale (gw.why.<kind>). */
const WHY_KINDS = ['gateway', 'port', 'vendor', 'service', 'upnp', 'banner', 'random_mac', 'name'];

/**
 * The text of one reason ("port 554 (RTSP)"). `tr` is the translator of the tab; a port that has a short name gets it
 * in brackets. A kind that is not known gives its own text, so a newer panel does not leave an empty place.
 */
export function whyText(w: WhyItem, tr: (key: string, vars?: Record<string, string | number>) => string): string {
  if (!WHY_KINDS.includes(w.kind)) return w.value ? `${w.kind}: ${w.value}` : w.kind;
  let value = w.value ?? '';
  if (w.kind === 'port') {
    const label = /^\d+$/.test(value) ? portLabel(Number(value)) : null;
    if (label) value = `${value} (${label})`;
  }
  return tr(`gw.why.${w.kind}`, { value });
}
