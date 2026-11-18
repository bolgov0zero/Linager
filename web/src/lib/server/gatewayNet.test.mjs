// Run with: node --test src/lib/server/gatewayNet.test.mjs (Node 22.18 or newer strips the types by itself).
// The cases mirror internal/gwnet/gwnet_test.go: the server is the authority, this is the early feedback.
import test from 'node:test';
import assert from 'node:assert/strict';
import { clash, hostCount, ipCompare, MAX_NETS, overlaps, parseNet, portLabel, portTag, split24, whyText } from './gatewayNet.ts';

test('good networks are masked and classified', () => {
  const ok = {
    '192.168.1.0/24': ['192.168.1.0/24', 'private'],
    '192.168.1.77/24': ['192.168.1.0/24', 'private'],
    '10.10.0.0/30': ['10.10.0.0/30', 'private'],
    '172.16.5.0/25': ['172.16.5.0/25', 'private'],
    '100.64.8.0/24': ['100.64.8.0/24', 'private'],
    '203.0.113.0/24': ['203.0.113.0/24', 'public'],
    '172.32.0.0/24': ['172.32.0.0/24', 'public'],
    '172.15.255.0/24': ['172.15.255.0/24', 'public'],
    '100.128.0.0/24': ['100.128.0.0/24', 'public'],
    '::ffff:10.0.0.0/120': ['10.0.0.0/24', 'private'],
  };
  for (const [input, [cidr, kind]] of Object.entries(ok)) {
    const r = parseNet(input);
    assert.equal(r.ok, true, input);
    assert.equal(r.cidr, cidr, input);
    assert.equal(r.kind, kind, input);
  }
  assert.equal(parseNet('192.168.1.77/24').aligned, false);
  assert.equal(parseNet('192.168.1.0/24').aligned, true);
  assert.equal(parseNet('  192.168.1.0/24 ').ok, true);
});

test('host counts follow the real size of the network', () => {
  assert.equal(parseNet('192.168.1.0/24').hosts, 254);
  assert.equal(parseNet('10.0.0.0/25').hosts, 126);
  assert.equal(parseNet('10.0.0.4/30').hosts, 2);
  assert.equal(hostCount(24), 254);
});

test('refused networks give the code of the server rule', () => {
  const bad = {
    '': 'empty',
    '   ': 'empty',
    '10.0.0.0': 'syntax',
    '10.0.0.0/33': 'syntax',
    '10.0.0.0/-1': 'syntax',
    '10.0.0.0/024': 'syntax',
    '10.0.0/24': 'syntax',
    '10.0.0.256/24': 'syntax',
    '010.0.0.0/24': 'syntax',
    'hello/24': 'syntax',
    'fe80::/64': 'ipv6',
    '2001:db8::/32': 'ipv6',
    '127.0.0.0/24': 'loopback',
    '127.1.2.0/24': 'loopback',
    '224.0.0.0/24': 'reserved',
    '239.1.1.0/24': 'reserved',
    '169.254.1.0/24': 'reserved',
    '0.0.0.0/24': 'reserved',
    '240.0.0.0/24': 'reserved',
    '255.255.255.0/24': 'reserved',
    '10.0.0.0/31': 'too_small',
    '10.0.0.1/32': 'too_small',
  };
  for (const [input, code] of Object.entries(bad)) {
    const r = parseNet(input);
    assert.equal(r.ok, false, input);
    assert.equal(r.error, code, input);
  }
});

test('a network larger than /24 reports its split', () => {
  let r = parseNet('10.20.0.0/22');
  assert.deepEqual([r.ok, r.error, r.bits, r.split, r.kind], [false, 'too_big', 22, 4, 'private']);
  assert.equal(parseNet('10.20.7.9/22').cidr, '10.20.4.0/22');
  r = parseNet('10.20.0.0/16');
  assert.equal(r.split, 256 > MAX_NETS ? 0 : 256);
  r = parseNet('10.20.0.0/20');
  assert.equal(r.split, 16);
  r = parseNet('10.20.0.0/20', 15);
  assert.equal(r.split, 0, 'the parts must fit in the limit');
  r = parseNet('10.0.0.0/8');
  assert.deepEqual([r.error, r.bits, r.split], ['too_big', 8, 0]);
  r = parseNet('203.0.112.0/23');
  assert.deepEqual([r.error, r.split, r.kind], ['too_big', 2, 'public']);
});

test('split24 follows Split24 of the server', () => {
  assert.deepEqual(split24('10.20.0.0/22'), ['10.20.0.0/24', '10.20.1.0/24', '10.20.2.0/24', '10.20.3.0/24']);
  assert.deepEqual(split24('10.20.7.9/23'), ['10.20.6.0/24', '10.20.7.0/24']);
  for (const p of ['10.0.0.0/8', '10.0.0.0/15', '10.0.0.0/24', '10.0.0.0/30', 'x']) assert.deepEqual(split24(p), [], p);
  assert.equal(split24('10.0.0.0/16').length, 256);
});

test('duplicates and overlaps are found', () => {
  assert.equal(overlaps('10.0.0.0/24', '10.0.0.0/24'), true);
  assert.equal(overlaps('10.0.0.0/24', '10.0.0.128/25'), true);
  assert.equal(overlaps('10.0.0.128/25', '10.0.0.0/24'), true);
  assert.equal(overlaps('10.0.0.0/24', '10.0.1.0/24'), false);
  assert.equal(overlaps('10.0.0.0/25', '10.0.0.128/25'), false);
  assert.deepEqual(clash('10.0.0.0/24', ['192.168.1.0/24', '10.0.0.0/24']), { cidr: '10.0.0.0/24', same: true });
  assert.deepEqual(clash('10.0.0.0/26', ['192.168.1.0/24', '10.0.0.0/24']), { cidr: '10.0.0.0/24', same: false });
  assert.equal(clash('10.0.5.0/24', ['10.0.0.0/24']), null);
});

test('addresses sort by octet, not as text', () => {
  const sorted = ['192.168.1.250', '192.168.1.9', '10.0.0.1', '192.168.1.100', '192.168.20.1', 'bad'].sort(ipCompare);
  assert.deepEqual(sorted, ['10.0.0.1', '192.168.1.9', '192.168.1.100', '192.168.1.250', '192.168.20.1', 'bad']);
});

test('port names', () => {
  assert.equal(portLabel(22), 'SSH');
  assert.equal(portLabel(9100), 'RAW');
  assert.equal(portLabel(8006), 'Proxmox');
  assert.equal(portLabel(12345), null);
  assert.equal(portTag(443), '443 HTTPS');
  assert.equal(portTag(12345), '12345');
});

test('reasons of a device type', () => {
  // the translator of the test just shows the key and the value
  const tr = (key, vars) => (vars ? `${key}:${vars.value}` : key);
  assert.equal(whyText({ kind: 'port', value: '554' }, tr), 'gw.why.port:554 (RTSP)');
  assert.equal(whyText({ kind: 'port', value: '12345' }, tr), 'gw.why.port:12345');
  assert.equal(whyText({ kind: 'vendor', value: 'Synology' }, tr), 'gw.why.vendor:Synology');
  assert.equal(whyText({ kind: 'gateway' }, tr), 'gw.why.gateway:');
  assert.equal(whyText({ kind: 'future', value: 'x' }, tr), 'future: x');
});
