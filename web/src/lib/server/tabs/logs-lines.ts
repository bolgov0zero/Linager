/** Lines kept in memory for the logs view. */
export const LOG_LIMIT = 2000;

export type LogTone = '' | 'err' | 'warn';

/** Colors a line by keyword: errors and failures red, warnings yellow. */
export function logTone(line: string): LogTone {
  const lower = line.toLowerCase();
  if (lower.includes('error') || lower.includes('fail')) return 'err';
  if (lower.includes('warn')) return 'warn';
  return '';
}

/** Turns binary stream chunks (UTF-8) into complete text lines; a partial line waits for the next chunk. */
export class LogLines {
  private readonly decoder = new TextDecoder('utf-8');
  private partial = '';

  push(chunk: ArrayBuffer): string[] {
    const text = this.partial + this.decoder.decode(new Uint8Array(chunk), { stream: true });
    const parts = text.split('\n');
    this.partial = parts.pop() ?? '';
    return parts.map((line) => (line.endsWith('\r') ? line.slice(0, -1) : line));
  }

  /** Returns the unfinished line (if any) when the stream ends. */
  flush(): string[] {
    const rest = this.partial + this.decoder.decode();
    this.partial = '';
    return rest ? [rest] : [];
  }
}
