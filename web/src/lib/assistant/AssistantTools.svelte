<script lang="ts">
  import { assistantApi, type AssistSession } from '../../api';
  import { tr } from '../../i18n';
  import ActionMenu from '../ui/ActionMenu.svelte';
  import type AssistantSession from './AssistantSession.svelte';
  import type { AssistInfo, AssistPhase } from './AssistantSession.svelte';
  import { sessionValue } from './nav';

  /**
   * The buttons of the Assistant: the session list, Ctrl+Alt+Del on the bar and the rest in a menu. Shared by the
   * window and the tab. The session that is picked is the value the owner passes to the screen.
   */
  let {
    serverId,
    session = $bindable('console'),
    phase,
    info = null,
    screen,
    onsettings,
    layout = 'tab',
    display = $bindable('fit'),
  }: {
    serverId: string;
    /** 'console' or the number of a Windows session. */
    session?: string;
    phase: AssistPhase;
    /** What the computer reports (the monitors and the picked one); null before the first picture. */
    info?: AssistInfo | null;
    screen: AssistantSession | null;
    /** The "Quality and frame rate" item: the owner shows the Manage tab. */
    onsettings: () => void;
    /** Where the screen is: a page tab (the picture is fitted or shown at 100%) or a floating window (the window can follow the picture). */
    layout?: 'tab' | 'window';
    /** How the picture and the window go together; the owner applies it. */
    display?: 'fit' | 'window' | 'max' | 'real';
  } = $props();

  const live = $derived(phase === 'open');

  let list = $state<AssistSession[]>([]);

  async function load(): Promise<void> {
    try {
      list = (await assistantApi.sessions(serverId)).sessions;
    } catch {
      // no list (an old agent, a short break): the console stays to pick, whatever is already shown stays
    }
  }

  // The list changes while the window is open (people sign in and out), so it is read again from time to time.
  $effect(() => {
    void serverId;
    void load();
    const timer = setInterval(() => void load(), 15000);
    return () => clearInterval(timer);
  });

  const typeName = (s: AssistSession): string => $tr(`assist.type.${s.type}`);
  const userName = (s: AssistSession): string => s.user || $tr('assist.noUser');

  /** "2 · RDP · user" with what is worth knowing after it. */
  function itemLabel(s: AssistSession): string {
    const note = !s.screen ? $tr('assist.noScreen') : s.type === 'rdp' && s.client ? s.client : '';
    return `${s.id} · ${typeName(s)} · ${userName(s)}${note ? ` (${note})` : ''}`;
  }

  const entries = $derived.by(() => {
    const out: { value: string; session?: AssistSession }[] = list.map((s) => ({ value: sessionValue(s), session: s }));
    // The console is always there to pick, even before the list came or when it could not be read.
    if (!out.some((e) => e.value === 'console')) out.unshift({ value: 'console' });
    return out;
  });

  const sessionItems = $derived(
    entries.map((e) => ({
      key: e.value,
      label: `${e.value === session ? '✓ ' : ''}${e.session ? itemLabel(e.session) : $tr('assist.console')}`,
      disabled: e.session ? !e.session.screen : false,
      onselect: () => (session = e.value),
    })),
  );

  const current = $derived(entries.find((e) => e.value === session)?.session);

  // Ctrl+Alt+Del can be sent by the service only into the console session.
  const canCad = $derived((current?.type ?? (session === 'console' ? 'console' : '')) === 'console');

  const monitorItems = $derived.by(() => {
    const s = screen;
    const list = info?.monitors ?? [];
    if (list.length < 2) return [];
    const picked = info?.monitor ?? -1;
    const mark = (on: boolean): string => (on ? '✓ ' : '');
    return [
      ...list.map((m, i) => ({
        key: `mon${i}`,
        label: `${mark(picked === i)}${$tr('assist.monitorN', { n: i + 1 })}${m.primary ? ` (${$tr('assist.monitorPrimary')})` : ''} · ${m.w}×${m.h}`,
        onselect: () => s?.selectMonitor(i),
      })),
      { key: 'monall', label: `${mark(picked === -1)}${$tr('assist.monitorAll')}`, onselect: () => s?.selectMonitor(-1) },
    ];
  });

  const displayItems = $derived.by(() => {
    const mark = (d: string): string => (display === d ? '✓ ' : '');
    const pick = (d: 'fit' | 'window' | 'max' | 'real') => () => (display = d);
    const list =
      layout === 'window'
        ? [
            { key: 'fit', label: `${mark('fit')}${$tr('assist.mode.fit')}`, onselect: pick('fit') },
            { key: 'window', label: `${mark('window')}${$tr('assist.mode.window')}`, onselect: pick('window') },
            { key: 'max', label: `${mark('max')}${$tr('assist.mode.max')}`, onselect: pick('max') },
          ]
        : [
            { key: 'fit', label: `${mark('fit')}${$tr('assist.mode.fit')}`, onselect: pick('fit') },
            { key: 'real', label: `${mark('real')}${$tr('assist.mode.real')}`, onselect: pick('real') },
          ];
    return [...list, { key: 'full', label: $tr('assist.mode.full'), onselect: () => screen?.fullscreen() }];
  });

  const items = $derived.by(() => {
    const s = screen;
    const clip = s?.clipboardAvailable() ?? false;
    return [
      { key: 'tm', label: $tr('assist.taskManager'), onselect: () => s?.combo(['ControlLeft', 'ShiftLeft', 'Escape']) },
      { key: 'win', label: $tr('assist.winKey'), onselect: () => s?.winKey() },
      { key: 'lock', label: $tr('assist.lock'), onselect: () => s?.combo(['MetaLeft', 'KeyL']) },
      { key: 'run', label: $tr('assist.run'), onselect: () => s?.combo(['MetaLeft', 'KeyR']) },
      { key: 'alttab', label: $tr('assist.altTab'), onselect: () => s?.combo(['AltLeft', 'Tab']) },
      ...monitorItems,
      { key: 'send', label: $tr('desk.clipSend'), disabled: !clip, onselect: () => void s?.sendClipboard() },
      { key: 'save', label: $tr('desk.clipSave'), disabled: !clip, onselect: () => s?.getClipboard() },
      { key: 'quality', label: $tr('assist.quality'), onselect: onsettings },
      { key: 'off', label: $tr('assist.disconnect'), onselect: () => s?.disconnect() },
    ];
  });
</script>

<ActionMenu label={$tr('assist.sessionBtn')} title={$tr('assist.sessionBtn')} items={sessionItems} />
<ActionMenu label="" icon="desktop" title={$tr('assist.display')} items={displayItems} />
{#if live}
  <button class="btn sm" type="button" title={canCad ? $tr('assist.cadHint') : $tr('assist.cadConsoleOnly')} disabled={!canCad} onclick={() => screen?.ctrlAltDel()}>Ctrl+Alt+Del</button>
  <ActionMenu label={$tr('assist.more')} {items} />
{/if}
