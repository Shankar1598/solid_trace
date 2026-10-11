import { QueryEditor } from '@/components/QueryEditor'
import type { QueryProblem } from '@/lib/logs/api'
import type { CompletionSources } from '@/lib/logs/completions'

/** Query → Builder needs asking: the stages the Builder can't show, or the Query's mistakes. */
export interface LeavingQuery {
  stages: string[]
  problems: QueryProblem[]
}

interface QueryBoxProps {
  /** The Project's slug, which a Query starts with as its source. */
  projectSlug: string
  text: string
  onChange: (text: string) => void
  onRun: () => void
  /** The mistakes of the Query last run, squiggled in the box and listed under it. */
  problems: QueryProblem[]
  /** What completions offer: the Fields and values of recent Logs, and the Project's source. */
  completions: CompletionSources
  leaving: LeavingQuery | null
  onDiscard: () => void
  onStay: () => void
}

/** Query mode's editable Query text, its mistakes, and the prompt to leave it. */
export function QueryBox({ projectSlug, text, onChange, onRun, problems, completions, leaving, onDiscard, onStay }: QueryBoxProps) {
  return (
    <div className="border-b bg-muted/30 px-3 py-2">
      <QueryEditor
        value={text}
        onChange={onChange}
        onRun={onRun}
        language="query"
        label="Query"
        placeholder={`['${projectSlug}']\n| where level == "error"\n| summarize count() by service`}
        problems={problems}
        completions={completions}
        maxLines={8}
        autoFocus
        describedBy={problems.length > 0 ? 'query-problems' : undefined}
      />
      {problems.length > 0 && (
        <div id="query-problems">
          {problems.map((p, i) => (
            <div key={i} className="mt-0.5 font-mono text-[11px] text-destructive">
              Line {p.line}, column {p.column}: {p.message}
            </div>
          ))}
        </div>
      )}
      {leaving && (
        <div className="mt-2 flex flex-wrap items-center gap-3 rounded-md border border-amber-400 bg-amber-50 p-2 text-xs dark:bg-amber-950/30">
          {/* Wide enough that on a phone the buttons wrap below it together. */}
          <span className="min-w-64 flex-1">
            {leaving.stages.length > 0 ? (
              <>The Builder can't show {leaving.stages.map((s, i) => (
                <code key={i} className="mx-0.5 rounded bg-background px-1">{s}</code>
              ))}.</>
            ) : leaving.problems.length > 0 ? (
              <>The Builder can't show a Query with mistakes (line {leaving.problems[0].line}, column {leaving.problems[0].column}: {leaving.problems[0].message}).</>
            ) : (
              <>The Builder can't show this Query.</>
            )}
            {' '}Switching discards your Query and returns to the last Builder filters.
          </span>
          <button onClick={onDiscard} className="rounded border px-2 py-1 hover:bg-background">Discard and switch</button>
          <button onClick={onStay} className="rounded border px-2 py-1 hover:bg-background">Stay in Query</button>
        </div>
      )}
    </div>
  )
}
