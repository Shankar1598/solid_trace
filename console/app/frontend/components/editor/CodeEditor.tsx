// The QueryEditor's CodeMirror, loaded on its own after the page has
// painted. components/QueryEditor.tsx is the way in.
import { useEffect, useRef } from 'react'
import { acceptCompletion, autocompletion, closeBrackets, closeBracketsKeymap, closeCompletion } from '@codemirror/autocomplete'
import { defaultKeymap, history, historyKeymap } from '@codemirror/commands'
import { PostgreSQL, sql } from '@codemirror/lang-sql'
import { bracketMatching, syntaxHighlighting } from '@codemirror/language'
import { setDiagnostics } from '@codemirror/lint'
import { Annotation, Compartment, EditorState, Prec } from '@codemirror/state'
import { EditorView, highlightSpecialChars, keymap, placeholder, tooltips } from '@codemirror/view'
import type { QueryEditorProps } from '../QueryEditor'
import { LINE_HEIGHT, PADDING_Y } from './size'
import { diagnostics, highlightStyle, optionClass, queryLanguage, querySource } from './queryLanguage'

// Marks the editor's own writes of the value it was given, which aren't edits.
const external = Annotation.define<boolean>()

/** The text box's name and what describes it, such as its mistakes. */
function contentAttributes(label: string, describedBy: string | undefined) {
  return EditorView.contentAttributes.of({ 'aria-label': label, ...(describedBy ? { 'aria-describedby': describedBy } : {}) })
}

export default function CodeEditor(props: QueryEditorProps) {
  const host = useRef<HTMLDivElement>(null)
  const view = useRef<EditorView | null>(null)
  const latest = useRef(props)
  latest.current = props
  const attributes = useRef(new Compartment())

  useEffect(() => {
    const { value, language, label, placeholder: hint, maxLines, describedBy } = latest.current
    const runQuery = (v: EditorView) => {
      closeCompletion(v)
      latest.current.onRun()
      return true
    }
    const editor = new EditorView({
      parent: host.current!,
      state: EditorState.create({
        doc: value,
        extensions: [
          // Ctrl+Enter runs everywhere, as the bar's hint says, and so does Cmd+Enter on a Mac.
          Prec.highest(keymap.of([
            { key: 'Mod-Enter', run: runQuery },
            { mac: 'Ctrl-Enter', run: runQuery },
          ])),
          // Escape closes completions first (autocompletion's keymap comes before this), then leaves the editor.
          keymap.of([
            { key: 'Escape', run: v => { v.contentDOM.blur(); return true } },
            { key: 'Tab', run: acceptCompletion },
            ...closeBracketsKeymap,
            ...historyKeymap,
            ...defaultKeymap,
          ]),
          history(),
          closeBrackets(),
          bracketMatching(),
          highlightSpecialChars(),
          EditorView.lineWrapping,
          language === 'sql' ? sql({ dialect: PostgreSQL, upperCaseKeywords: true }) : queryLanguage,
          syntaxHighlighting(highlightStyle),
          autocompletion({
            icons: false,
            optionClass: language === 'query' ? optionClass : undefined,
            override: language === 'query' ? [querySource(() => latest.current.completions ?? { fields: [] })] : undefined,
          }),
          // Out of the explorer's clipped panels.
          tooltips({ parent: document.body }),
          hint ? placeholder(hint) : [],
          attributes.current.of(contentAttributes(label, describedBy)),
          EditorView.updateListener.of(update => {
            if (update.docChanged && !update.transactions.some(t => t.annotation(external))) {
              latest.current.onChange(update.state.doc.toString())
            }
          }),
          editorTheme(maxLines),
        ],
      }),
    })
    view.current = editor
    if (latest.current.autoFocus) {
      editor.focus()
      editor.dispatch({ selection: { anchor: editor.state.doc.length } })
    }
    return () => {
      editor.destroy()
      view.current = null
    }
    // One editor per mount: the value and mistakes follow below.
  }, [])

  // A value set from outside, such as Back or a pivot, replaces the text.
  useEffect(() => {
    const editor = view.current
    if (!editor || editor.state.doc.toString() === props.value) return
    editor.dispatch({ changes: { from: 0, to: editor.state.doc.length, insert: props.value }, annotations: external.of(true) })
  }, [props.value])

  useEffect(() => {
    view.current?.dispatch({ effects: attributes.current.reconfigure(contentAttributes(props.label, props.describedBy)) })
  }, [props.label, props.describedBy])

  // The mistakes squiggle where they are until the next run; the editor moves them along with edits.
  useEffect(() => {
    const editor = view.current
    if (!editor) return
    editor.dispatch(setDiagnostics(editor.state, diagnostics(editor.state.doc.toString(), props.problems ?? [])))
  }, [props.problems])

  return <div ref={host} className="min-w-0" />
}

/** Sized and coloured as the app is: tokens from css/globals.css, so it follows dark and light. */
function editorTheme(maxLines: number) {
  return EditorView.theme({
    '&': {
      backgroundColor: 'transparent',
      color: 'var(--foreground)',
      fontSize: '12px',
    },
    '&.cm-focused': { outline: 'none' },
    '.cm-scroller': {
      fontFamily: 'inherit',
      lineHeight: `${LINE_HEIGHT}px`,
      maxHeight: `${maxLines * LINE_HEIGHT + 2 * PADDING_Y}px`,
      overflow: 'auto',
    },
    '.cm-content': {
      padding: `${PADDING_Y}px 0`,
      minHeight: `${2 * LINE_HEIGHT + 2 * PADDING_Y}px`,
      caretColor: 'var(--foreground)',
    },
    '.cm-line': { padding: '0 8px' },
    '.cm-placeholder': { color: 'var(--muted-foreground)' },
    '.cm-cursor': { borderLeftColor: 'var(--foreground)' },
    '&.cm-focused .cm-matchingBracket': { backgroundColor: 'var(--accent)', outline: '1px solid var(--ring)' },
    '.cm-nonmatchingBracket': { color: 'var(--destructive)' },
    '.cm-lintRange-error': {
      backgroundImage: 'none',
      textDecoration: 'underline wavy var(--destructive)',
      textDecorationSkipInk: 'none',
      textUnderlineOffset: '3px',
    },
    '.cm-tooltip': {
      backgroundColor: 'var(--popover)',
      color: 'var(--popover-foreground)',
      border: '1px solid var(--border)',
      boxShadow: '0 4px 12px rgb(0 0 0 / 0.15)',
      fontSize: '12px',
    },
    '.cm-tooltip.cm-tooltip-autocomplete > ul': {
      fontFamily: 'inherit',
      maxHeight: '16rem',
      maxWidth: 'min(32rem, calc(100vw - 1rem))',
      minWidth: '14rem',
    },
    '.cm-tooltip.cm-tooltip-autocomplete > ul > li': { display: 'flex', padding: '2px 8px', lineHeight: '18px' },
    '.cm-tooltip-autocomplete ul li[aria-selected]': { backgroundColor: 'var(--accent)', color: 'var(--accent-foreground)' },
    '.cm-tooltip-autocomplete ul completion-section': {
      padding: '4px 8px 2px',
      borderBottom: 'none',
      color: 'var(--muted-foreground)',
      fontSize: '11px',
      opacity: '1',
    },
    '.cm-completionLabel': { color: 'inherit' },
    '.cm-completionMatchedText': { textDecoration: 'none', fontWeight: '700' },
    '.cm-completionDetail': { fontStyle: 'normal', color: 'var(--muted-foreground)', whiteSpace: 'pre' },
    '.cm-st-call .cm-completionDetail': { marginLeft: '0' },
    '.cm-st-form .cm-completionDetail': { marginLeft: '1ch' },
    '.cm-st-typed .cm-completionDetail': { marginLeft: 'auto', paddingLeft: '2ch' },
    '.cm-tooltip.cm-completionInfo': { padding: '4px 8px', maxWidth: '18rem' },
    '.cm-diagnostic': { padding: '4px 8px', fontFamily: 'inherit' },
    '.cm-diagnostic-error': { borderLeft: '3px solid var(--destructive)' },
  })
}
