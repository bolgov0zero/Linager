<script lang="ts">
  import { assistantApi, type AssistSession } from '../../api';
  import { tr } from '../../i18n';
  import ActionMenu from '../ui/ActionMenu.svelte';
  import type AssistantSession from './AssistantSession.svelte';
  import type { AssistPhase } from './AssistantSession.svelte';
  import { sessionValue } from './nav';

  /**
   * The buttons of the Assistant: the session list, Ctrl+Alt+Del on the bar and the rest in a menu. Shared by the
   * window and the tab. The session that is picked is the value the owner passes to the screen.
   */
  let {
    serverId,
    session = $bindable('console'),
    phase,
    screen,
    onsettings,
  }: {
    serverId: string;
    /** 'console' or the number of a Windows session. */
    session?: string;
    phase: AssistPhase;
    screen: AssistantSession | null;
    /** The "Quality and frame rate" item: the owner shows the Manage tab. */
    onsettings: () => void;
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
  const currentLabel = $derived.by(() => {
    if (current) return $tr('assist.current', { id: current.id, type: typeName(current), user: userName(current) });
    return session === 'console' ? $tr('assist.console') : $tr('assist.sessionN', { n: session });
  });

  const items = $derived.by(() => {
    const s = screen;
    return [
      { key: 'tm', label: $tr('assist.taskManager'), onselect: () => s?.combo(['ControlLeft', 'ShiftLeft', 'Escape']) },
      { key: 'win', label: $tr('assist.winKey'), onselect: () => s?.winKey() },
      { key: 'lock', label: $tr('assist.lock'), onselect: () => s?.combo(['MetaLeft', 'KeyL']) },
      { key: 'run', label: $tr('assist.run'), onselect: () => s?.combo(['MetaLeft', 'KeyR']) },
      { key: 'alttab', label: $tr('assist.altTab'), onselect: () => s?.combo(['AltLeft', 'Tab']) },
      { key: 'fit', label: $tr('assist.scaleFit'), onselect: () => s?.setScale('fit') },
      { key: 'real', label: $tr('assist.scaleReal'), onselect: () => s?.setScale('real') },
      { key: 'full', label: $tr('assist.fullscreen'), onselect: () => s?.fullscreen() },
      { key: 'quality', label: $tr('assist.quality'), onselect: onsettings },
      { key: 'off', label: $tr('assist.disconnect'), onselect: () => s?.disconnect() },
    ];
  });
</script>

<ActionMenu label={currentLabel} items={sessionItems} />
{#if live}
  <button class="btn sm" type="button" title={$tr('assist.cadHint')} onclick={() => screen?.ctrlAltDel()}>Ctrl+Alt+Del</button>
  <ActionMenu label={$tr('assist.more')} {items} />
{/if}
