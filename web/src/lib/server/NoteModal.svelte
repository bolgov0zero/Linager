<script lang="ts">
  import { api, isUnauthorized, type ServerDetail } from '../../api';
  import { locale, tr } from '../../i18n';
  import { renderMarkdown } from '../markdown';
  import Modal from '../Modal.svelte';
  import { showToast } from '../toast';
  import PixIcon from '../ui/PixIcon.svelte';
  import { formatDateTime } from './format';

  const MAX_CHARS = 20000;

  let {
    server,
    canEdit,
    onclose,
    onsaved,
    onauthlost,
  }: {
    server: ServerDetail;
    /** Administrators change the note; the other roles only read it. */
    canEdit: boolean;
    onclose: () => void;
    /** The note was saved: the page keeps the new text, so the button and the next opening show it. */
    onsaved: (note: { note: string; note_updated_at?: number; note_updated_by?: string }) => void;
    onauthlost: () => void;
  } = $props();

  let editing = $state(false);
  let draft = $state('');
  let saving = $state(false);

  const note = $derived(server.note ?? '');
  const html = $derived(renderMarkdown(note));
  const dirty = $derived(editing && draft !== note);

  function startEdit(): void {
    draft = note;
    editing = true;
  }

  /** Throws the changes away and returns to the text; the window stays open. */
  function cancel(): void {
    editing = false;
    draft = '';
  }

  async function save(): Promise<void> {
    if (saving) return;
    if (draft === note) {
      cancel();
      return;
    }
    saving = true;
    try {
      onsaved(await api.serverNoteSet(server.id, draft));
      showToast($tr('srv.note.saved'));
      editing = false;
      draft = '';
    } catch (error) {
      if (isUnauthorized(error)) {
        onauthlost();
        return;
      }
      showToast($tr('srv.note.saveFailed'));
    } finally {
      saving = false;
    }
  }

  /** The cross and the click outside: they must not eat an unsaved text. */
  function close(): void {
    if (dirty) {
      showToast($tr('srv.note.unsaved'));
      return;
    }
    onclose();
  }

  function focusOnMount(node: HTMLTextAreaElement): void {
    node.focus();
    node.setSelectionRange(node.value.length, node.value.length);
  }

  function onKey(event: KeyboardEvent): void {
    if ((event.ctrlKey || event.metaKey) && event.key === 'Enter') {
      event.preventDefault();
      void save();
    }
  }
</script>

<Modal title={$tr('srv.note.title')} labelId="srv-note-title" onclose={close} onescape={() => (editing ? cancel() : close())}>
  {#snippet actions()}
    {#if canEdit && !editing}
      <button class="btn pencil" type="button" aria-label={$tr('srv.note.edit')} title={$tr('srv.note.edit')} onclick={startEdit}>
        <PixIcon name="pencil" size={16} />
      </button>
    {/if}
  {/snippet}

  {#if editing}
    <label class="field">
      <span class="sr">{$tr('srv.note.label')}</span>
      <textarea
        class="input mono"
        rows="14"
        maxlength={MAX_CHARS}
        spellcheck="true"
        bind:value={draft}
        use:focusOnMount
        onkeydown={onKey}
      ></textarea>
    </label>
    <div class="row between">
      <span class="muted small">{$tr('srv.note.hint')}</span>
      <span class="muted small">{$tr('srv.note.counter', { n: draft.length, max: MAX_CHARS })}</span>
    </div>
    <div class="row end">
      <button class="btn" type="button" disabled={saving} onclick={cancel}>{$tr('common.cancel')}</button>
      <button class="btn primary" type="button" disabled={saving} onclick={() => void save()}>{$tr('srv.note.save')}</button>
    </div>
  {:else if note.trim() === ''}
    <p class="muted empty">
      {$tr('srv.note.empty')}{#if canEdit} {$tr('srv.note.emptyEdit')}{/if}
    </p>
  {:else}
    <div class="md">{@html html}</div>
    {#if server.note_updated_at}
      <p class="muted small meta">
        {$tr('srv.note.meta', { date: formatDateTime(server.note_updated_at, $locale), who: server.note_updated_by ?? '' })}
      </p>
    {/if}
  {/if}
</Modal>

<style>
  .pencil {
    padding: 0 10px;
  }

  .field {
    display: flex;
    flex-direction: column;
  }

  .sr {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip: rect(0 0 0 0);
  }

  textarea {
    width: 100%;
    min-height: 240px;
    height: auto;
    resize: vertical;
    line-height: 1.45;
    padding: 10px;
  }

  .row {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
  }

  .row.between {
    justify-content: space-between;
  }

  .row.end {
    justify-content: flex-end;
  }

  .empty {
    margin: 0;
    padding: 16px 0;
  }

  .meta {
    margin: 0;
  }

  /* The rendered note: modest headings, the project's mono text, code and quotes in the frame style. */
  .md {
    min-height: 120px;
    overflow-wrap: anywhere;
  }

  .md :global(p) {
    margin: 0 0 10px;
  }

  .md :global(h3),
  .md :global(h4),
  .md :global(h5) {
    margin: 14px 0 6px;
    font-size: 16px;
    line-height: 1.2;
  }

  .md :global(h3) {
    font-size: 18px;
  }

  .md :global(h5) {
    color: var(--muted);
    text-transform: uppercase;
    letter-spacing: 0.04em;
    font-size: 13px;
  }

  .md :global(ul),
  .md :global(ol) {
    margin: 0 0 10px;
    padding-left: 22px;
  }

  .md :global(li) {
    margin: 2px 0;
  }

  .md :global(a) {
    color: var(--accent);
  }

  .md :global(code) {
    padding: 0 4px;
    background: var(--seg-off);
  }

  .md :global(pre) {
    margin: 0 0 10px;
    padding: 10px;
    border: 2px solid var(--seg-off);
    background: var(--bg);
    overflow-x: auto;
  }

  .md :global(pre code) {
    padding: 0;
    background: none;
  }

  .md :global(blockquote) {
    margin: 0 0 10px;
    padding: 4px 12px;
    border-left: 4px solid var(--seg-off);
    color: var(--muted);
  }

  .md :global(hr) {
    margin: 12px 0;
    border: 0;
    border-top: 2px dashed var(--seg-off);
  }

  .md :global(.task) {
    color: var(--accent);
  }

  .md :global(del) {
    color: var(--muted);
  }

  .md > :global(:last-child) {
    margin-bottom: 0;
  }
</style>
