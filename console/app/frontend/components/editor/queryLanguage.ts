// The Query language for CodeMirror: colours from the lexer, completions from
// lib/logs/completions.ts, and validate's mistakes as diagnostics. Loaded
// only with the editor.
import { startCompletion, type Completion, type CompletionSource } from '@codemirror/autocomplete'
import { HighlightStyle, StreamLanguage } from '@codemirror/language'
import type { Diagnostic } from '@codemirror/lint'
import type { EditorView } from '@codemirror/view'
import { tags } from '@lezer/highlight'
import type { QueryProblem } from '@/lib/logs/api'
import { completeQuery, type CompletionSources, type QueryCompletion } from '@/lib/logs/completions'
import { problemSpan } from '@/lib/logs/problemSpans'
import { readToken, startState, type LexState, type TokenKind } from '@/lib/logs/queryLexer'

const TAGS: Record<TokenKind, string> = {
  stage: 'keyword',
  keyword: 'keyword',
  operator: 'operator',
  function: 'function',
  name: 'name',
  field: 'field',
  string: 'string',
  number: 'number',
  timespan: 'number',
  literal: 'literal',
  parameter: 'parameter',
  comment: 'comment',
  pipe: 'pipe',
  punctuation: 'punctuation',
  invalid: 'invalid',
}

export const queryLanguage = StreamLanguage.define<LexState>({
  name: 'solidtrace-query',
  startState,
  copyState: state => ({ ...state }),
  token(stream, state) {
    const { kind, end } = readToken(stream.string, stream.pos, state)
    stream.pos = end
    return kind && TAGS[kind]
  },
  tokenTable: {
    keyword: tags.keyword,
    operator: tags.operatorKeyword,
    function: tags.function(tags.variableName),
    name: tags.special(tags.string),
    field: tags.propertyName,
    string: tags.string,
    number: tags.number,
    literal: tags.literal,
    parameter: tags.variableName,
    comment: tags.lineComment,
    pipe: tags.separator,
    punctuation: tags.punctuation,
    invalid: tags.invalid,
  },
  languageData: { commentTokens: { line: '//' } },
})

/** The editor's colours, from the app's tokens (css/globals.css), in both themes. */
export const highlightStyle = HighlightStyle.define([
  { tag: [tags.keyword, tags.separator], color: 'var(--syntax-keyword)' },
  { tag: [tags.operatorKeyword, tags.operator, tags.compareOperator, tags.logicOperator, tags.arithmeticOperator], color: 'var(--syntax-operator)' },
  { tag: [tags.function(tags.variableName), tags.standard(tags.name)], color: 'var(--syntax-function)' },
  { tag: tags.string, color: 'var(--syntax-string)' },
  { tag: tags.special(tags.string), color: 'var(--syntax-name)' },
  { tag: [tags.number, tags.literal, tags.bool, tags.null], color: 'var(--syntax-literal)' },
  { tag: tags.variableName, color: 'var(--syntax-parameter)' },
  { tag: [tags.lineComment, tags.blockComment, tags.comment], color: 'var(--muted-foreground)', fontStyle: 'italic' },
  { tag: tags.punctuation, color: 'var(--muted-foreground)' },
  { tag: tags.invalid, color: 'var(--destructive)' },
  { tag: [tags.typeName, tags.special(tags.variableName)], color: 'var(--syntax-function)' },
])

/** completeQuery as a CodeMirror source, reading the Fields and values from `sources` when asked. */
export function querySource(sources: () => CompletionSources): CompletionSource {
  return context => {
    const answer = completeQuery(context.state.doc.toString(), context.pos, sources(), context.explicit)
    if (!answer) return null
    const ranks = new Map<string, number>()
    for (const o of answer.options) if (o.section && !ranks.has(o.section)) ranks.set(o.section, ranks.size)
    return {
      from: answer.from,
      // Asked again on each key (no validFor), so a word typed in full closes the list.
      options: answer.options.map((o, i) => completion(o, i, ranks)),
    }
  }
}

function completion(o: QueryCompletion, index: number, ranks: Map<string, number>): Completion {
  const text = o.apply ?? o.label
  return {
    label: o.label,
    detail: o.detail,
    info: o.info,
    type: o.type,
    // In the order given: the editor would otherwise sort ties by name.
    boost: Math.max(-99, -index),
    section: o.section ? { name: o.section, rank: ranks.get(o.section) } : undefined,
    apply: (view: EditorView, _completion: Completion, from: number, to: number) => {
      view.dispatch({
        changes: { from, to, insert: text },
        selection: { anchor: from + (o.cursor ?? text.length) },
        userEvent: 'input.complete',
        scrollIntoView: true,
      })
      // What comes next: a stage's Fields, a Field's operators, an operator's values.
      if (o.reopen) setTimeout(() => startCompletion(view))
    },
  }
}

/** A function's arguments and a stage's form read on from its name; a type sits at the right. */
export function optionClass(completion: Completion): string {
  if (completion.type === 'function') return 'cm-st-call'
  return completion.type === 'stage' || completion.type === 'keyword' ? 'cm-st-form' : 'cm-st-typed'
}

/** Validate's mistakes as diagnostics on `text`: a squiggle each, its message on hover. */
export function diagnostics(text: string, problems: QueryProblem[]): Diagnostic[] {
  return problems.map(p => ({ ...problemSpan(text, p), severity: 'error', message: p.message }))
}
