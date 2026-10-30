import { CircleSlash, FileText, Play, Timer } from 'lucide-react'
import { Button } from '@/components/ui/button'
import type { AbsoluteRange } from '@/lib/logs/explorerState'
import { refusalView, type Refusal } from '@/lib/logs/refusal'
import { rangeLabel } from '@/lib/timeRange'

export interface RefusalNoticeProps {
  refusal: Refusal
  /** Runs the Query over the range given. */
  onRange: (range: AbsoluteRange) => void
  /** Adds file == <file> to the Query and runs it, or null while no filter can be added. */
  onFile: ((file: string) => void) | null
  /** Runs the Query as written, past the budget. */
  onRunAnyway: () => void
}

/**
 * EventStore's refusal of a run, in place of results: why, how much the
 * Query would read against what it may, and ways to read less, each a button
 * that runs the narrower Query. A refusal with only a message shows only
 * that; one while another large Query runs offers no Run anyway.
 */
export function RefusalNotice({ refusal, onRange, onFile, onRunAnyway }: RefusalNoticeProps) {
  const view = refusalView(refusal)
  const files = view.files
  return (
    <div role="status" className="h-full overflow-y-auto p-6 text-sm sm:p-10" data-refusal>
      <div className="mx-auto w-fit max-w-2xl space-y-5">
        <div className="flex gap-2.5">
          <CircleSlash className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" aria-hidden />
          <p className="whitespace-pre-wrap break-words">{view.message}</p>
        </div>

        {view.estimate && (
          <div className="space-y-1.5" data-refusal-estimate>
            <div className="flex flex-wrap justify-between gap-x-4 text-xs">
              <span>Would read about <span className="font-medium">{view.estimate.bytes}</span> in {view.estimate.logs}</span>
              <span className="text-muted-foreground">A Query that matches text may read {view.estimate.budget}</span>
            </div>
            {/* The whole bar is the read; its first part is what the budget allows. */}
            <div className="h-1.5 w-full bg-destructive/50" aria-hidden>
              <div className="h-full bg-muted-foreground/60" style={{ width: `${view.estimate.budgetShare * 100}%` }} />
            </div>
          </div>
        )}

        {view.range && (
          <div className="space-y-2">
            <h3 className="text-xs text-muted-foreground">Run it over the largest range that fits</h3>
            <Button variant="outline" className="h-auto py-1.5" onClick={() => onRange(view.range!.range)}>
              <Timer className="h-3.5 w-3.5" aria-hidden />
              <span>Last {view.range.length}</span>
              <span className="font-normal text-muted-foreground">{rangeLabel(view.range.range, new Date())} · {view.range.bytes}</span>
            </Button>
          </div>
        )}

        {files.length > 0 && (
          <div className="space-y-2">
            <h3 className="text-xs text-muted-foreground">Or filter on one of the busiest files</h3>
            <ul className="flex flex-col items-start gap-1.5">
              {files.map(({ service, file, logs, bytes }) => {
                const detail = <span className="font-normal text-muted-foreground">{service} · {logs} · {bytes}</span>
                return (
                  <li key={`${service}/${file ?? ''}`} className="max-w-full">
                    {file !== null && onFile ? (
                      <Button variant="outline" className="h-auto max-w-full py-1.5" title={`Add file == "${file}" and run`} onClick={() => onFile(file)}>
                        <FileText className="h-3.5 w-3.5" aria-hidden />
                        <span className="truncate font-mono">{file}</span>
                        {detail}
                      </Button>
                    ) : (
                      // Logs with no file: a filter on file == "" would miss them.
                      <span className="inline-flex items-center gap-1.5 px-2.5 py-1.5 text-xs">
                        <FileText className="h-3.5 w-3.5 text-muted-foreground" aria-hidden />
                        <span className={file === null ? 'italic text-muted-foreground' : 'font-mono'}>{file ?? 'no file'}</span>
                        {detail}
                      </span>
                    )}
                  </li>
                )
              })}
            </ul>
          </div>
        )}

        {view.runAnyway && (
          <div className="flex flex-wrap items-center gap-x-3 gap-y-1.5 border-t pt-4">
            <Button variant="outline" onClick={onRunAnyway}><Play className="h-3.5 w-3.5" aria-hidden /> Run anyway</Button>
            <span className="text-xs text-muted-foreground">Runs the Query as written, at a lower priority. It may still time out.</span>
          </div>
        )}
      </div>
    </div>
  )
}
