/** Moves between the Assistant and its settings across the server page and the floating windows. */

/** Opens a tab of a server page (the address of the page carries the tab). */
export function gotoServerTab(serverName: string, tab: 'assistant' | 'manage'): void {
  location.hash = `#srv-${encodeURIComponent(serverName)}/${encodeURIComponent(tab)}`;
}

let wanted: { serverId: string; session: string } | null = null;

/** The Assistant tab of the server opens with this session (the table of sessions on the Manage tab asks for it). */
export function wantAssistSession(serverId: string, session: string): void {
  wanted = { serverId, session };
}

/** The session asked for with wantAssistSession, once; 'console' when nobody asked. */
export function takeAssistSession(serverId: string): string {
  const w = wanted;
  wanted = null;
  return w && w.serverId === serverId ? w.session : 'console';
}

/** The value the Assistant takes for a Windows session: 'console' for the one at the screen of the computer, else its number. */
export function sessionValue(s: { id: number; console: boolean }): string {
  return s.console ? 'console' : String(s.id);
}
