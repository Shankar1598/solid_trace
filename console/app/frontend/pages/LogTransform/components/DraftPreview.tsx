import { useState } from 'react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Segmented } from '@/components/ui/segmented'
import { Textarea } from '@/components/ui/textarea'
import { previewProgram } from '@/lib/logTransform/api'
import { pastedLogs, type PreviewAnswer, type PreviewResult } from '@/lib/logTransform/preview'
import { inspectorJson } from '@/lib/logs/inspector'

// How many results show at first, and how many more each "Show more" adds.
const RESULTS_SHOWN = 50

type RunOn = 'recent' | 'pasted'

/** The last preview: running, its answer for the draft it ran, or why there's none. */
type Previewed = { running: true } | { program: string; answer: PreviewAnswer } | { failed: string }

/**
 * Runs the draft on the Project's recent arrivals, or on Logs pasted as JSON,
 * and shows each Log before and after with the kept, dropped and failed
 * counts. Nothing is stored. Any member can.
 */
export function DraftPreview({ orgSlug, projectSlug, draft }: { orgSlug: string; projectSlug: string; draft: string }) {
  const [runOn, setRunOn] = useState<RunOn>('recent')
  const [pasted, setPasted] = useState('')
  const [previewed, setPreviewed] = useState<Previewed | null>(null)
  const [shown, setShown] = useState(RESULTS_SHOWN)

  const run = () => {
    let logs: object[] | undefined
    if (runOn === 'pasted') {
      const parsed = pastedLogs(pasted)
      if ('error' in parsed) {
        setPreviewed({ failed: parsed.error })
        return
      }
      logs = parsed.logs
    }
    const program = draft
    setPreviewed({ running: true })
    setShown(RESULTS_SHOWN)
    previewProgram({ orgSlug, projectSlug, program, logs })
      .then(answer => setPreviewed({ program, answer }))
      .catch(error => setPreviewed({ failed: error instanceof Error ? error.message : String(error) }))
  }

  const running = previewed !== null && 'running' in previewed

  return (
    <Card>
      <CardHeader>
        <CardTitle>Preview</CardTitle>
        <CardDescription>
          Runs the program in the editor on this Project's recent arrivals, the newest Logs of each Service as they arrived, or on Logs you
          paste, and shows each Log before and after. Nothing is stored.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="flex flex-wrap items-center gap-3">
          <Segmented
            label="Run on"
            value={runOn}
            onChange={setRunOn}
            options={[{ value: 'recent', label: 'Recent arrivals' }, { value: 'pasted', label: 'Pasted Logs' }]}
          />
          <Button onClick={run} disabled={running}>{running ? 'Previewing…' : 'Preview'}</Button>
        </div>
        {runOn === 'pasted' && (
          <Textarea
            aria-label="Logs to preview on"
            value={pasted}
            onChange={event => setPasted(event.target.value)}
            placeholder={'[{"message": "GET /health 200", "service": "web"}]'}
            className="min-h-24 font-mono"
            spellCheck={false}
          />
        )}
        {previewed !== null && !running && (
          'failed' in previewed ? (
            <p role="alert" className="text-sm text-destructive">{previewed.failed}</p>
          ) : (
            <PreviewAnswerView
              answer={previewed.answer}
              stale={previewed.program !== draft}
              shown={shown}
              onShowMore={() => setShown(shown + RESULTS_SHOWN)}
            />
          )
        )}
      </CardContent>
    </Card>
  )
}

function PreviewAnswerView({ answer, stale, shown, onShowMore }: {
  answer: PreviewAnswer; stale: boolean; shown: number; onShowMore: () => void
}) {
  const staleLine = stale && <p className="text-xs text-muted-foreground">The program has changed since this preview.</p>
  if ('check' in answer) {
    return (
      <div className="space-y-2">
        {staleLine}
        <p className="text-sm font-medium text-destructive">The program doesn't compile, so it can't be previewed.</p>
        {answer.check.errors.map((p, i) => (
          <pre key={i} className="overflow-x-auto bg-muted p-2 font-mono text-xs whitespace-pre-wrap">{p.text || p.message}</pre>
        ))}
      </div>
    )
  }

  const { results, kept, dropped, failed } = answer.preview
  if (results.length === 0) {
    return (
      <p className="text-sm text-muted-foreground">
        No recent arrivals to preview on: EventStore keeps them from when it starts. Paste Logs to preview on instead.
      </p>
    )
  }
  return (
    <div className="space-y-4">
      {staleLine}
      <p className="text-sm">
        {results.length.toLocaleString()} {results.length === 1 ? 'Log' : 'Logs'}: {kept.toLocaleString()} kept, {dropped.toLocaleString()} dropped,
        {' '}{failed.toLocaleString()} failed.
      </p>
      <ol className="space-y-4">
        {results.slice(0, shown).map((result, i) => <ResultItem key={i} result={result} />)}
      </ol>
      {results.length > shown && (
        <Button variant="outline" size="sm" onClick={onShowMore}>
          Show {Math.min(RESULTS_SHOWN, results.length - shown)} more
        </Button>
      )}
    </div>
  )
}

/** One Log before and after, side by side; or before and what stopped it. */
function ResultItem({ result }: { result: PreviewResult }) {
  return (
    <li aria-label="Preview result" className="space-y-2 border p-3">
      {'after' in result ? (
        <Badge variant="ok">Kept</Badge>
      ) : 'dropped' in result ? (
        <Badge variant="neutral">Dropped</Badge>
      ) : (
        <Badge variant="error">Failed</Badge>
      )}
      <div className="grid gap-3 md:grid-cols-2">
        <section aria-label="Before" className="min-w-0 space-y-1">
          <h4 className="text-xs text-muted-foreground">Before</h4>
          <LogJson json={inspectorJson(result.before)} />
        </section>
        <section aria-label="After" className="min-w-0 space-y-1">
          <h4 className="text-xs text-muted-foreground">After</h4>
          {'after' in result ? (
            <LogJson json={inspectorJson(result.after)} />
          ) : 'dropped' in result ? (
            <p className="text-sm text-muted-foreground">Dropped: the program aborts on it, so it wouldn't be stored.</p>
          ) : (
            <div className="space-y-1">
              <p className="text-sm text-muted-foreground">
                The program fails on it, so it would be stored as it arrived, with the error in
                {' '}<code className="font-mono text-xs">solidtrace.transform_error</code>:
              </p>
              <pre className="overflow-x-auto bg-destructive/10 p-2 font-mono text-xs whitespace-pre-wrap text-destructive">{result.error}</pre>
            </div>
          )}
        </section>
      </div>
    </li>
  )
}

function LogJson({ json }: { json: string }) {
  return <pre className="max-h-96 overflow-auto bg-muted p-2 font-mono text-xs whitespace-pre-wrap break-all">{json}</pre>
}
