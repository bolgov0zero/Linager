import type { Extension } from '@codemirror/state';
import { StreamLanguage, type StreamParser } from '@codemirror/language';

/** Files above this size are opened without syntax highlighting to keep typing responsive. */
export const HIGHLIGHT_MAX_BYTES = 1024 * 1024;

type Loader = () => Promise<Extension>;

const legacy = (load: () => Promise<StreamParser<unknown>>): Loader => async () => StreamLanguage.define(await load());

const php: Loader = async () => (await import('@codemirror/lang-php')).php();
const html: Loader = async () => (await import('@codemirror/lang-html')).html();
const css: Loader = async () => (await import('@codemirror/lang-css')).css();
const js: Loader = async () => (await import('@codemirror/lang-javascript')).javascript();
const ts: Loader = async () => (await import('@codemirror/lang-javascript')).javascript({ typescript: true });
const json: Loader = async () => (await import('@codemirror/lang-json')).json();
const yaml: Loader = async () => (await import('@codemirror/lang-yaml')).yaml();
const xml: Loader = async () => (await import('@codemirror/lang-xml')).xml();
const sql: Loader = async () => (await import('@codemirror/lang-sql')).sql();
const markdown: Loader = async () => (await import('@codemirror/lang-markdown')).markdown();
const python: Loader = async () => (await import('@codemirror/lang-python')).python();
const shell = legacy(async () => (await import('@codemirror/legacy-modes/mode/shell')).shell);
const nginx = legacy(async () => (await import('@codemirror/legacy-modes/mode/nginx')).nginx);
const dockerfile = legacy(async () => (await import('@codemirror/legacy-modes/mode/dockerfile')).dockerFile);
const toml = legacy(async () => (await import('@codemirror/legacy-modes/mode/toml')).toml);
const properties = legacy(async () => (await import('@codemirror/legacy-modes/mode/properties')).properties);

const BY_EXTENSION: Record<string, Loader> = {
  php, phtml: php, php5: php, php7: php, inc: php,
  html, htm: html, xhtml: html, vue: html,
  css, scss: css, less: css,
  js, mjs: js, cjs: js, jsx: js, ts, tsx: ts,
  json, jsonc: json,
  yml: yaml, yaml,
  xml, svg: xml, xsl: xml, plist: xml,
  sql,
  md: markdown, markdown,
  py: python,
  sh: shell, bash: shell, zsh: shell,
  toml,
  ini: properties, cfg: properties, properties, env: properties, service: properties, timer: properties,
  socket: properties, mount: properties, target: properties, desktop: properties,
};

const BY_NAME: Record<string, Loader> = {
  '.bashrc': shell, '.profile': shell, '.bash_profile': shell, '.bash_aliases': shell, '.bash_logout': shell,
  '.zshrc': shell, '.zprofile': shell,
  dockerfile,
  '.env': properties, 'sshd_config': properties, 'ssh_config': properties, 'fstab': properties, 'hosts': properties,
  'interfaces': properties,
};

/** The highlighting extension for a file, or null when it is a plain text file. */
export async function languageFor(path: string): Promise<Extension | null> {
  const name = (path.split('/').pop() ?? path).toLowerCase();
  const byName = BY_NAME[name];
  if (byName) return byName();
  if (name.endsWith('.conf') && /\/nginx\//.test(path)) return nginx();
  if (name === 'nginx.conf') return nginx();
  if (name.startsWith('dockerfile.') || name.endsWith('.dockerfile')) return dockerfile();
  const dot = name.lastIndexOf('.');
  const byExt = dot >= 0 ? BY_EXTENSION[name.slice(dot + 1)] : undefined;
  if (byExt) return byExt();
  if (name.endsWith('.conf')) return properties();
  return null;
}
