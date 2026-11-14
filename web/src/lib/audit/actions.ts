/** Known audit action keys. Each one has an `audit.action.<key>` translation; other keys are shown raw. */
export const KNOWN_ACTIONS: ReadonlySet<string> = new Set([
  'auth.login', 'auth.logout', 'auth.totp.enable', 'auth.totp.disable',
  'enroll.approve', 'enroll.reject', 'server.delete', 'server.reboot',
  'service.start', 'service.stop', 'service.restart', 'service.enable', 'service.disable',
  'process.kill',
  'files.write', 'files.mkdir', 'files.create', 'files.delete', 'files.upload', 'files.download',
  'network.apply', 'network.config.apply', 'network.confirm', 'network.rollback', 'network.draft.save', 'network.draft.reset',
  'firewall.apply', 'firewall.confirm', 'firewall.rollback', 'firewall.draft.save', 'firewall.draft.reset',
  'docker.container.start', 'docker.container.stop', 'docker.container.restart', 'docker.container.remove',
  'docker.image.remove',
  'update.check', 'update.channel', 'update.prefetch', 'update.server', 'update.agents',
  'stream.open', 'stream.close',
  'metrics.export',
  'dashboard.add', 'dashboard.remove', 'dashboard.reorder',
  'group.create', 'group.rename', 'group.delete', 'group.assign',
]);

export interface Option {
  value: string;
  key: string;
}

/** Action group filter. The value is sent as the `action` parameter. */
export const ACTION_GROUPS: Option[] = [
  { value: '', key: 'audit.filter.all' },
  { value: 'auth.', key: 'audit.group.auth' },
  { value: 'enroll.,server.', key: 'audit.group.servers' },
  { value: 'network.', key: 'audit.group.network' },
  { value: 'firewall.', key: 'audit.group.firewall' },
  { value: 'files.', key: 'audit.group.files' },
  { value: 'service.', key: 'audit.group.services' },
  { value: 'stream.', key: 'audit.group.stream' },
  { value: 'update.', key: 'audit.group.updates' },
  { value: 'docker.', key: 'audit.group.docker' },
  { value: 'process.', key: 'audit.group.process' },
  { value: 'metrics.', key: 'audit.group.metrics' },
  { value: 'dashboard.', key: 'audit.group.dashboard' },
];

/** Result filter. Denial results (bad_key, locked, rate_limited) are all shown as "denied". */
export const RESULT_OPTIONS: Option[] = [
  { value: '', key: 'audit.filter.all' },
  { value: 'ok', key: 'audit.result.ok' },
  { value: 'failed', key: 'audit.result.failed' },
  { value: 'bad_key', key: 'audit.result.denied' },
  { value: 'locked', key: 'audit.result.denied' },
  { value: 'rate_limited', key: 'audit.result.denied' },
];
