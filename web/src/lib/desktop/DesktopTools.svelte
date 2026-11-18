<script lang="ts">
  import { tr } from '../../i18n';
  import ActionMenu from '../ui/ActionMenu.svelte';
  import type DesktopSession from './DesktopSession.svelte';
  import type { DeskPhase } from './DesktopSession.svelte';

  /** The buttons of the remote desktop: Ctrl+Alt+Del on the bar, the rest in a menu. Shared by the window and the tab. */
  let {
    session,
    phase,
    autoResize = $bindable(true),
  }: { session: DesktopSession | null; phase: DeskPhase; autoResize?: boolean } = $props();

  const live = $derived(phase === 'open');

  const items = $derived.by(() => {
    const s = session;
    const list: { key: string; label: string; disabled?: boolean; onselect: () => void }[] = [
      { key: 'tm', label: $tr('desk.taskManager'), onselect: () => s?.combo(['ControlLeft', 'ShiftLeft', 'Escape']) },
      { key: 'win', label: $tr('desk.winKey'), onselect: () => s?.winKey() },
      { key: 'lock', label: $tr('desk.lock'), onselect: () => s?.combo(['MetaLeft', 'KeyL']) },
      { key: 'run', label: $tr('desk.run'), onselect: () => s?.combo(['MetaLeft', 'KeyR']) },
      { key: 'alttab', label: $tr('desk.altTab'), onselect: () => s?.combo(['AltLeft', 'Tab']) },
    ];
    const clip = s?.clipboardAvailable() ?? false;
    list.push(
      { key: 'send', label: $tr('desk.clipSend'), disabled: !clip, onselect: () => void s?.sendClipboard() },
      { key: 'save', label: $tr('desk.clipSave'), disabled: !clip, onselect: () => void s?.saveClipboard() },
      { key: 'fit', label: $tr('desk.scaleFit'), onselect: () => s?.setScale('fit') },
      { key: 'real', label: $tr('desk.scaleReal'), onselect: () => s?.setScale('real') },
      { key: 'auto', label: `${autoResize ? '✓ ' : ''}${$tr('desk.autoResize')}`, onselect: () => (autoResize = !autoResize) },
      { key: 'full', label: $tr('desk.fullscreen'), onselect: () => s?.fullscreen() },
      { key: 'off', label: $tr('desk.disconnect'), onselect: () => s?.disconnect() },
    );
    return list;
  });
</script>

{#if live}
  <button class="btn sm" type="button" title={$tr('desk.cadHint')} onclick={() => session?.ctrlAltDel()}>Ctrl+Alt+Del</button>
  <ActionMenu label={$tr('desk.more')} {items} />
{/if}
