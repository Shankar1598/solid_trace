import { Component, Suspense, lazy, useEffect, useRef, type ReactNode } from 'react'
import type { ProgramMark } from '@/lib/logTransform/program'
import type { QueryProblem } from '@/lib/logs/api'
import type { CompletionSources } from '@/lib/logs/completions'
import type { ShortcutId } from '@/lib/shortcuts'
import { cn } from '@/lib/utils'
import { LINE_HEIGHT, PADDING_Y } from './editor/size'
import { useShortcut } from './useShortcut'

// CodeMirror loads on its own, so the page paints without waiting for it.
const CodeEditor = lazy(() => import('./editor/CodeEditor'))

export interface QueryEditorProps {
  value: string
  onChange: (value: string) => void
  /** Cmd/Ctrl+Enter. */
  onRun?: () => void
  /** A Query, coloured and completed as one, SQL, or a Log transform's VRL program, as plain text. */
  language: 'query' | 'sql' | 'vrl'
  /** The text box's accessible name, such as "Query". */
  label: string
  placeholder?: string
  /** A Query's mistakes, squiggled where validate placed them. */
  problems?: QueryProblem[]
  /** Errors and warnings already placed in the text, such as a program's from its check. */
  marks?: ProgramMark[]
  /** What a Query's completions offer: the Fields and values of recent Logs, and its source. */
  completions?: CompletionSources
  /** The box grows with its text up to this many lines, then scrolls. */
  maxLines: number
  /** The box is at least this many lines tall (default 2). */
  minLines?: number
  /** The text can be read, selected and copied, but not changed. */
  readOnly?: boolean
  autoFocus?: boolean
  /** A shortcut that focuses it with the cursor at the end, such as / for the explorer's Query. */
  focusShortcut?: ShortcutId
  /** The id of the text that describes the box, such as its mistakes. */
  describedBy?: string
  className?: string
}

/**
 * The editor for Queries and SQL in the Logs explorer, Dashboards and
 * Monitors, and for a Project's Log transform program: highlighting,
 * completions for Queries, mistakes as squiggles, and Cmd/Ctrl+Enter to run.
 * Esc leaves it.
 */
export function QueryEditor(props: QueryEditorProps) {
  const invalid = (props.problems?.length ?? 0) > 0 || (props.marks ?? []).some(m => m.severity === 'error')
  return (
    <div
      data-invalid={invalid || undefined}
      className={cn(
        'border bg-background font-mono text-[12px] focus-within:ring-1 focus-within:ring-primary',
        invalid && 'border-destructive',
        props.className,
      )}
    >
      <Unloaded fallback={<PlainEditor {...props} />}>
        <Suspense fallback={<Loading {...props} />}>
          <CodeEditor {...props} />
        </Suspense>
      </Unloaded>
    </div>
  )
}

/**
 * Shows `fallback` when CodeMirror fails to load, as when a tab open since
 * before a deploy asks for a chunk the new one no longer has, so the page
 * stays and the box still edits and runs.
 */
class Unloaded extends Component<{ fallback: ReactNode; children: ReactNode }, { failed: boolean }> {
  state = { failed: false }

  static getDerivedStateFromError() {
    return { failed: true }
  }

  render() {
    return this.state.failed ? this.props.fallback : this.props.children
  }
}

/** A plain text box in the editor's place: no colours or completions, but it edits, runs and leaves the same keys. */
function PlainEditor({ value, onChange, onRun, label, placeholder, maxLines, minLines = 2, autoFocus, focusShortcut, describedBy, readOnly }: QueryEditorProps) {
  const box = useRef<HTMLTextAreaElement>(null)
  // As the editor does: focused with the cursor at the end.
  const focusAtEnd = () => {
    if (!box.current) return false
    box.current.focus()
    box.current.setSelectionRange(box.current.value.length, box.current.value.length)
  }
  useEffect(() => {
    if (autoFocus) focusAtEnd()
    // On mount only, as the editor.
  }, [])
  useShortcut(focusShortcut ?? 'focus-query', focusShortcut ? focusAtEnd : null)
  return (
    <textarea
      ref={box}
      value={value}
      spellCheck={false}
      onChange={e => onChange(e.target.value)}
      onKeyDown={e => {
        if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
          e.preventDefault()
          onRun?.()
        } else if (e.key === 'Escape') {
          e.currentTarget.blur()
        }
      }}
      aria-label={label}
      aria-describedby={describedBy}
      readOnly={readOnly}
      placeholder={placeholder}
      className="block w-full resize-none bg-transparent px-2 font-[inherit] outline-none [field-sizing:content] placeholder:text-muted-foreground"
      style={{
        lineHeight: `${LINE_HEIGHT}px`,
        paddingTop: PADDING_Y,
        paddingBottom: PADDING_Y,
        minHeight: minLines * LINE_HEIGHT + 2 * PADDING_Y,
        maxHeight: maxLines * LINE_HEIGHT + 2 * PADDING_Y,
      }}
    />
  )
}

/** The text as it will show, in a box of the editor's size, until CodeMirror has loaded. */
function Loading({ value, placeholder, maxLines, minLines = 2 }: QueryEditorProps) {
  return (
    <pre
      aria-hidden
      className={cn('overflow-hidden px-2 font-[inherit] break-words whitespace-pre-wrap', value === '' && 'text-muted-foreground')}
      style={{
        lineHeight: `${LINE_HEIGHT}px`,
        paddingTop: PADDING_Y,
        paddingBottom: PADDING_Y,
        minHeight: minLines * LINE_HEIGHT + 2 * PADDING_Y,
        maxHeight: maxLines * LINE_HEIGHT + 2 * PADDING_Y,
      }}
    >
      {value === '' ? placeholder : value}
    </pre>
  )
}
