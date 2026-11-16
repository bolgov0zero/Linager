<script lang="ts">
  import { onMount } from 'svelte';
  import { Compartment, EditorState } from '@codemirror/state';
  import {
    EditorView,
    drawSelection,
    highlightActiveLine,
    highlightActiveLineGutter,
    keymap,
    lineNumbers,
  } from '@codemirror/view';
  import { defaultKeymap, history, historyKeymap, indentWithTab } from '@codemirror/commands';
  import { bracketMatching, HighlightStyle, indentOnInput, syntaxHighlighting } from '@codemirror/language';
  import { highlightSelectionMatches, searchKeymap } from '@codemirror/search';
  import { tags as t } from '@lezer/highlight';
  import { HIGHLIGHT_MAX_BYTES, languageFor } from './languages';

  /**
   * Text editor with syntax highlighting (CodeMirror 6). The language is picked from the file path and loaded on
   * demand; colours come from the interface's CSS variables, so both themes work.
   */
  let {
    value = $bindable(),
    path,
    label,
    wrap = false,
    onsave,
    onready,
  }: {
    value: string;
    path: string;
    label: string;
    /** Wrap long lines instead of scrolling sideways. */
    wrap?: boolean;
    onsave?: () => void;
    /** Receives a function that moves the focus into the editor. */
    onready?: (focus: () => void) => void;
  } = $props();

  let host = $state<HTMLDivElement | undefined>(undefined);
  let view: EditorView | undefined;
  const language = new Compartment();
  const wrapping = new Compartment();

  const theme = EditorView.theme({
    '&': { height: '100%', color: 'var(--ink)', backgroundColor: 'var(--surface)', fontSize: '13px' },
    '&.cm-focused': { outline: '2px solid var(--accent)', outlineOffset: '-2px' },
    '.cm-scroller': { fontFamily: 'var(--font-body)', lineHeight: '1.45', overflow: 'auto' },
    '.cm-content': { caretColor: 'var(--ink)' },
    '.cm-cursor': { borderLeftColor: 'var(--ink)' },
    '.cm-gutters': { backgroundColor: 'var(--bg)', color: 'var(--muted)', border: 'none', borderRight: '2px solid var(--seg-off)' },
    '.cm-activeLine': { backgroundColor: 'color-mix(in srgb, var(--accent) 8%, transparent)' },
    '.cm-activeLineGutter': { backgroundColor: 'color-mix(in srgb, var(--accent) 14%, transparent)', color: 'var(--ink)' },
    '.cm-selectionBackground, &.cm-focused .cm-selectionBackground, ::selection': {
      backgroundColor: 'color-mix(in srgb, var(--accent) 30%, transparent)',
    },
    '.cm-selectionMatch': { backgroundColor: 'color-mix(in srgb, var(--warn) 30%, transparent)' },
    '&.cm-focused .cm-matchingBracket': { backgroundColor: 'color-mix(in srgb, var(--good) 30%, transparent)', outline: 'none' },
    '.cm-panels': { backgroundColor: 'var(--surface)', color: 'var(--ink)', borderTop: '2px solid var(--line)' },
    '.cm-panels input, .cm-panels button': { font: 'inherit', color: 'var(--ink)' },
    '.cm-searchMatch': { backgroundColor: 'color-mix(in srgb, var(--warn) 35%, transparent)' },
  });

  const highlight = HighlightStyle.define([
    { tag: [t.keyword, t.modifier, t.controlKeyword, t.operatorKeyword], color: 'var(--accent)' },
    { tag: [t.string, t.special(t.string), t.regexp], color: 'var(--good)' },
    { tag: [t.number, t.bool, t.null, t.atom], color: 'var(--warn)' },
    { tag: [t.comment, t.lineComment, t.blockComment], color: 'var(--muted)', fontStyle: 'italic' },
    { tag: [t.function(t.variableName), t.function(t.propertyName), t.className, t.typeName], color: 'var(--series-2)' },
    { tag: [t.tagName, t.heading, t.definition(t.variableName)], color: 'var(--crit)' },
    { tag: [t.attributeName, t.propertyName, t.labelName], color: 'var(--series-2)' },
    { tag: [t.meta, t.processingInstruction, t.documentMeta], color: 'var(--muted)' },
    { tag: [t.link, t.url], color: 'var(--accent)', textDecoration: 'underline' },
    { tag: t.strong, fontWeight: 'bold' },
    { tag: t.emphasis, fontStyle: 'italic' },
    { tag: t.invalid, color: 'var(--crit)' },
  ]);

  onMount(() => {
    if (!host) return;
    view = new EditorView({
      parent: host,
      state: EditorState.create({
        doc: value,
        extensions: [
          lineNumbers(),
          highlightActiveLineGutter(),
          highlightActiveLine(),
          drawSelection(),
          history(),
          indentOnInput(),
          bracketMatching(),
          highlightSelectionMatches(),
          syntaxHighlighting(highlight),
          theme,
          language.of([]),
          wrapping.of(wrap ? EditorView.lineWrapping : []),
          EditorView.contentAttributes.of({ 'aria-label': label, spellcheck: 'false', autocapitalize: 'off' }),
          keymap.of([
            { key: 'Mod-s', preventDefault: true, run: () => (onsave?.(), true) },
            indentWithTab,
            ...searchKeymap,
            ...historyKeymap,
            ...defaultKeymap,
          ]),
          EditorView.updateListener.of((u) => {
            if (u.docChanged) value = u.state.doc.toString();
          }),
        ],
      }),
    });
    onready?.(() => view?.focus());
    void loadLanguage();
    return () => view?.destroy();
  });

  async function loadLanguage(): Promise<void> {
    if (!view || value.length > HIGHLIGHT_MAX_BYTES) return;
    try {
      const ext = await languageFor(path);
      if (ext) view?.dispatch({ effects: language.reconfigure(ext) });
    } catch {
      // the language chunk could not be loaded: the file stays plain text
    }
  }

  // The wrap switch changes while the editor is open.
  $effect(() => {
    const on = wrap;
    view?.dispatch({ effects: wrapping.reconfigure(on ? EditorView.lineWrapping : []) });
  });

  // The text is replaced from outside (reload after a conflict): put it into the editor.
  $effect(() => {
    const next = value;
    if (view && next !== view.state.doc.toString()) {
      view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: next } });
    }
  });
</script>

<div class="cm-host" bind:this={host}></div>

<style>
  .cm-host {
    flex: 1;
    min-height: 0;
    border: 2px solid var(--line);
    overflow: hidden;
  }

  .cm-host :global(.cm-editor) {
    height: 100%;
  }

  /* Phones: the editor window is full screen, so the frame around the text would only cost width. */
  @media (max-width: 720px) {
    .cm-host {
      border-width: 2px 0;
    }
  }
</style>
