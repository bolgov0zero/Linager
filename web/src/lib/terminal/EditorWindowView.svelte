<script lang="ts">
  import { untrack } from 'svelte';
  import { api } from '../../api';
  import { get } from 'svelte/store';
  import { locale, t, tr, type Vars } from '../../i18n';
  import { errorText, isStatus, openErrorText } from '../server/tabs/files-util';
  import { showToast } from '../toast';
  import WindowFrame from './WindowFrame.svelte';
  import { closeTermWindow, type TermWindow } from './windows';

  let { win, top }: { win: TermWindow; top: boolean } = $props();

  /** Plain translate function for the helpers shared with the files tab. */
  function T(key: string, vars?: Vars): string {
    return t(key, get(locale), vars);
  }

  // The window is opened with the file content already loaded; from then on it owns its text.
  const first = untrack(() => ({ text: win.initialText ?? '', mtime: win.initialMtime ?? 0 }));
  const key = untrack(() => win.key);
  const serverId = untrack(() => win.serverId);
  const path = untrack(() => win.path ?? '');

  let text = $state(first.text);
  let saved = $state(first.text);
  let mtime = $state(first.mtime);
  let conflict = $state(false);
  let askClose = $state(false);
  let saving = $state(false);
  let focusEditor: (() => void) | undefined;

  const dirty = $derived(text !== saved);

  function close(): void {
    if (dirty) askClose = true;
    else closeTermWindow(key);
  }

  async function save(): Promise<void> {
    if (saving || !dirty) return;
    const sent = text;
    saving = true;
    try {
      const res = await api.fileSave(serverId, path, sent, mtime);
      saved = sent;
      mtime = res.mtime;
      showToast(T('files.saved'));
    } catch (e) {
      if (isStatus(e, 409)) conflict = true;
      else showToast(errorText(e, T));
    } finally {
      saving = false;
    }
  }

  async function reload(): Promise<void> {
    try {
      const f = await api.fileContent(serverId, path);
      text = f.content;
      saved = f.content;
      mtime = f.mtime;
      conflict = false;
      askClose = false;
    } catch (e) {
      showToast(openErrorText(e, T));
    }
  }

</script>

<WindowFrame {win} {top} ariaLabel={`${win.name} · ${win.serverName ?? ''}`} onclose={close} onrefocus={() => focusEditor?.()}>
  {#snippet title()}
    <span class="dot" class:on={dirty} aria-hidden="true"></span>
    <span class="label">{win.name}</span>
    <span class="muted status">· {win.serverName} · {path}</span>
  {/snippet}
  {#snippet tools()}
    <button class="btn sm" type="button" disabled={!dirty || saving} onclick={save}>
      {saving ? $tr('files.editor.saving') : $tr('files.editor.save')}
    </button>
  {/snippet}

  <div class="ed">
    {#if conflict}
      <div class="strip" role="alert">
        <p>{$tr('files.err.conflict')}</p>
        <div class="row end">
          <button class="btn" type="button" onclick={() => (conflict = false)}>{$tr('common.cancel')}</button>
          <button class="btn primary" type="button" onclick={reload}>{$tr('files.editor.reload')}</button>
        </div>
      </div>
    {:else if askClose}
      <div class="strip" role="alert">
        <p>{$tr('files.editor.unsaved')}</p>
        <div class="row end">
          <button class="btn" type="button" onclick={() => (askClose = false)}>{$tr('files.editor.stay')}</button>
          <button class="btn danger" type="button" onclick={() => closeTermWindow(key)}>{$tr('files.editor.discard')}</button>
        </div>
      </div>
    {/if}
    <!-- The editor (CodeMirror) is loaded when the first file is opened, not with the page. -->
    {#await import('../editor/CodeEditor.svelte') then { default: CodeEditor }}
      <CodeEditor bind:value={text} {path} label={win.name} onsave={() => void save()} onready={(f) => (focusEditor = f)} />
    {/await}
    <span class="small muted">{dirty ? $tr('files.editor.dirty') : $tr('files.editor.clean')}</span>
  </div>
</WindowFrame>

<style>
  .ed {
    display: flex;
    flex-direction: column;
    gap: 8px;
    height: 100%;
    min-height: 0;
  }

  .strip {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 10px 12px;
    border: 2px solid var(--warn);
  }

  .strip p {
    margin: 0;
  }
</style>
