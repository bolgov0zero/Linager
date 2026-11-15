// A readable view of an nft rule: action, protocol, port, source and the rest.

export type ViewAction = 'accept' | 'drop' | 'reject' | 'other';

export interface RuleView {
  action: ViewAction;
  /** tcp, udp, icmp, ... in upper case, or '' for "any". */
  proto: string;
  /** 22 or "80, 443"; '' when the rule has no port. */
  port: string;
  /** An address, a network or @list; '' for "any". */
  source: string;
  /** An interface the rule is limited to (iif / iifname). */
  iface: string;
  /** The comment of the rule. */
  comment: string;
  /** What did not fit the columns: connection state, limits, flags, destination. */
  extra: string;
}

const SYSTEM_RE = /^ct state (?:\{\s*)?established,\s*related(?:\s*\})?(?: counter(?: packets \d+ bytes \d+)?)? accept$/;

/** The rule that lets the replies of existing connections through; the agent puts it first in every chain. */
export function isSystemRule(text: string): boolean {
  return SYSTEM_RE.test(text.trim().replace(/\s+/g, ' '));
}

function list(value: string): string {
  return value
    .replace(/[{}]/g, '')
    .split(',')
    .map((x) => x.trim())
    .filter(Boolean)
    .join(', ');
}

/** Splits an nft rule into the columns of the table. Anything unknown stays in `extra`, nothing is lost. */
export function parseRule(text: string): RuleView {
  let t = ` ${text.trim().replace(/\s+/g, ' ')} `;
  const take = (re: RegExp): RegExpMatchArray | null => {
    const m = t.match(re);
    if (m) t = t.replace(re, ' ');
    return m;
  };

  const comment = take(/\bcomment "([^"]*)"/)?.[1] ?? '';
  take(/\bcounter(?: packets \d+ bytes \d+)?\b/);
  const verdict = take(/\b(accept|drop|reject)\b/)?.[1] as ViewAction | undefined;

  let proto = '';
  let port = '';
  const dport = take(/\b(tcp|udp)\s+dport\s+(\{[^}]*\}|\S+)/);
  if (dport) {
    proto = dport[1];
    port = list(dport[2]);
  } else {
    const l4 = take(/\bmeta\s+l4proto\s+(\{[^}]*\}|\S+)/) ?? take(/\bip6?\s+protocol\s+(\{[^}]*\}|\S+)/) ?? take(/\bip6\s+nexthdr\s+(\{[^}]*\}|\S+)/);
    if (l4) proto = list(l4[1]);
  }

  let source = '';
  const saddr = take(/\bip6?\s+saddr\s+(\{[^}]*\}|\S+)/);
  if (saddr) source = list(saddr[1]);
  const iface = take(/\biif(?:name)?\s+"?([\w.*-]+)"?/)?.[1] ?? '';

  const state = take(/\bct\s+state\s+(\{[^}]*\}|\S+)/);
  let extra = t.trim();
  if (state) extra = `ct state ${list(state[1]).replace(/, /g, ',')}${extra ? ' · ' + extra : ''}`;

  return { action: verdict ?? 'other', proto: proto.toUpperCase(), port, source, iface, comment, extra };
}
