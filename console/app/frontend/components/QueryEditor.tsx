import { Suspense, lazy } from 'react'
import type { QueryProblem } from '@/lib/logs/api'
import type { CompletionSources } from '@/lib/logs/completions'
import { cn } from '@/lib/utils'
import { LINE_HEIGHT, PADDING_Y } from './editor/size'

// CodeMirror loads on its own, so the page paints without waiting for it.
const CodeEditor = lazy(() => import('./editor/CodeEditor'))

export interface QueryEditorProps {
  value: string
  onChange: (value: string) => void
  /** Cmd/Ctrl+Enter. */
  onRun: () => void
  /** A Query, coloured and completed as one, or SQL. */
  language: 'query' | 'sql'
  /** The text box's accessible name, such as "Query". */
  label: string
  placeholder?: string
  /** A Query's mistakes, squiggled where validate placed them. */
  problems?: QueryProblem[]
  /** What a Query's completions offer: the Fields and values of recent Logs, and its source. */
  completions?: CompletionSources
  /** The box grows with its text up to this many lines, then scrolls. */
  maxLines: number
  autoFocus?: boolean
  /** The id of the text that describes the box, such as its mistakes. */
  describedBy?: string
  className?: string
}

/**
 * The editor for Queries and SQL in the Logs explorer, Dashboards and
 * Monitors: highlighting, completions for Queries, validate's mistakes as
 * squiggles, and Cmd/Ctrl+Enter to run. Esc leaves it.
 */
export function QueryEditor(props: QueryEditorProps) {
  const invalid = (props.problems?.length ?? 0) > 0
  return (
    <div
      data-invalid={invalid || undefined}
      className={cn(
        'border bg-background font-mono text-[12px] focus-within:ring-1 focus-within:ring-primary',
        invalid && 'border-destructive',
        props.className,
      )}
    >
      <Suspense fallback={<Loading {...props} />}>
        <CodeEditor {...props} />
      </Suspense>
    </div>
  )
}

/** The text as it will show, in a box of the editor's size, until CodeMirror has loaded. */
function Loading({ value, placeholder, maxLines }: QueryEditorProps) {
  return (
    <pre
      aria-hidden
      className={cn('overflow-hidden px-2 font-[inherit] break-words whitespace-pre-wrap', value === '' && 'text-muted-foreground')}
      style={{
        lineHeight: `${LINE_HEIGHT}px`,
        paddingTop: PADDING_Y,
        paddingBottom: PADDING_Y,
        minHeight: 2 * LINE_HEIGHT + 2 * PADDING_Y,
        maxHeight: maxLines * LINE_HEIGHT + 2 * PADDING_Y,
      }}
    >
      {value === '' ? placeholder : value}
    </pre>
  )
}
