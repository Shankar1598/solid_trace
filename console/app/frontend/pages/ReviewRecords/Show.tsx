import { Link, usePage } from '@inertiajs/react'
import { format } from 'date-fns'
import { AlertTriangle } from 'lucide-react'
import type { ReactNode } from 'react'
import { EvidenceFileList } from '@/components/EvidenceFileList'
import DashboardLayout from '@/components/layouts/DashboardLayout'
import { PageHeader } from '@/components/PageHeader'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent } from '@/components/ui/card'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { agentSessionPath } from '@/lib/agentSessions'
import type { EvidenceFile } from '@/lib/evidenceFiles'
import {
  decisionsOf,
  eventWords,
  foundByText,
  givenText,
  missedByTheReviews,
  prHref,
  recordPath,
  reviewersText,
  type RecordFinding,
  type ReviewRecord,
  type ReviewRecordRow,
  type ReviewRound,
} from '@/lib/reviewRecords'
import type { SharedProps } from '@/types'

type ReviewRecordProps = {
  event_store: { available: boolean; error?: string }
  review_record: ReviewRecordRow | null
  record: ReviewRecord | null
  rounds: ReviewRound[]
  list_path: string
  evidence_files: EvidenceFile[]
}

const OUTCOME_BADGES: Record<string, 'ok' | 'warn' | 'error' | 'neutral' | 'info'> = {
  fixed: 'ok', open: 'error', deferred: 'warn', disputed: 'info', ignored_by_person: 'neutral', ignored_trivial: 'neutral',
}

/**
 * One Review record: the story of the PR, its review runs, each finding
 * with the decisions on it in order, the confirmations, and the other
 * rounds of the PR. Every text is an agent's or a PR commenter's, so the
 * page shows it as text, never as HTML or Markdown.
 */
export default function ReviewRecordShow({ review_record: view }: { review_record: ReviewRecordProps }) {
  const row = view.review_record
  const record = view.record
  const title = row ? row.pr || 'No PR' : 'Review record'
  return (
    <DashboardLayout>
      <PageHeader
        breadcrumbs={[{ label: 'Review records', href: view.list_path }, { label: title }]}
        title={<span className="break-all" data-title>{title}</span>}
        tabTitle={`Review record ${title}`}
        badge={row && <Badge variant="count">Round {row.round}</Badge>}
        description={row?.title}
      />
      {!view.event_store.available && (
        <Card className="border-destructive/40" data-notice="event-store-error">
          <CardContent className="flex items-start gap-2 py-4 text-sm text-destructive">
            <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" aria-hidden />
            {view.event_store.error}
          </CardContent>
        </Card>
      )}
      {row && record && (
        <div className="space-y-6">
          <Header row={row} />
          <EvidenceFileList files={view.evidence_files} />
          <Story record={record} />
          <Reviews record={record} />
          <Findings findings={record.findings ?? []} />
          <Confirmations record={record} />
          <Rounds rounds={view.rounds} shown={row.id} listPath={view.list_path} />
        </div>
      )}
    </DashboardLayout>
  )
}

function Section({ title, name, children }: { title: string; name: string; children: ReactNode }) {
  return (
    <section aria-label={title} data-section={name} className="space-y-2">
      <h2 className="text-sm font-semibold">{title}</h2>
      {children}
    </section>
  )
}

function Header({ row }: { row: ReviewRecordRow }) {
  const { current_org } = usePage<SharedProps>().props
  const href = prHref(row.pr)
  const facts: [string, ReactNode][] = [
    ['PR', href
      ? <a href={href} target="_blank" rel="noreferrer noopener" className="break-all underline underline-offset-2">{row.pr}</a>
      : <span className="break-all">{row.pr || <span className="text-muted-foreground">No PR opened</span>}</span>],
    ['Stored', <time dateTime={row.stored_at}>{format(new Date(row.stored_at), 'MMM d, yyyy HH:mm')}</time>],
    ['Agent session', <Link href={agentSessionPath(current_org?.slug ?? '', row.session_id)} className="font-mono text-xs underline underline-offset-2">{row.session_id}</Link>],
    ['Findings', `${row.findings} (${row.fixed} fixed, ${row.missed} missed by the reviews)`],
    ['Confirmations not given', String(row.confirmations_not_given)],
  ]
  return (
    <Card data-section="review-record-header">
      <CardContent className="grid gap-x-6 gap-y-2 py-4 text-sm sm:grid-cols-2 lg:grid-cols-3">
        {facts.map(([label, value]) => (
          <div key={label} className="min-w-0">
            <div className="text-xs text-muted-foreground">{label}</div>
            <div className="break-words">{value}</div>
          </div>
        ))}
      </CardContent>
    </Card>
  )
}

function Story({ record }: { record: ReviewRecord }) {
  const story = record.story ?? {}
  const parts: [string, string[]][] = [
    ['Changes', story.changes ?? []],
    ['Reviews', story.reviews ?? []],
    ['After the reviews', story.after_review ?? []],
    ['Sessions', story.sessions ?? []],
  ]
  return (
    <Section title="Story" name="story">
      <Card>
        <CardContent className="space-y-3 py-4 text-sm">
          {story.started_from && <p className="whitespace-pre-wrap">{story.started_from}</p>}
          {parts.filter(([, lines]) => lines.length > 0).map(([label, lines]) => (
            <div key={label}>
              <div className="text-xs text-muted-foreground">{label}</div>
              <ul className="list-disc space-y-0.5 pl-5">
                {lines.map((line, i) => <li key={i} className="whitespace-pre-wrap">{line}</li>)}
              </ul>
            </div>
          ))}
        </CardContent>
      </Card>
    </Section>
  )
}

function Reviews({ record }: { record: ReviewRecord }) {
  const reviews = record.reviews ?? []
  return (
    <Section title="Reviews" name="reviews">
      {reviews.length === 0 ? <p className="text-sm text-muted-foreground">No review runs.</p> : (
        <div className="overflow-x-auto border">
          <Table aria-label="Reviews">
            <TableHeader>
              <TableRow>
                <TableHead className="text-right">Run</TableHead>
                <TableHead>Skill</TableHead>
                <TableHead>Stage and step</TableHead>
                <TableHead>Reviewers</TableHead>
                <TableHead className="max-md:hidden">Verifier</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {reviews.map((review, i) => (
                <TableRow key={`${i}:${review.run}`} data-run={review.run}>
                  <TableCell className="text-right font-mono text-xs tabular-nums">{review.run}</TableCell>
                  <TableCell className="text-xs">
                    {review.skill ?? '–'}
                    {review.trigger && <span className="text-muted-foreground"> ({review.trigger})</span>}
                    {review.skipped && <div className="text-muted-foreground">Skipped: {review.skipped}</div>}
                  </TableCell>
                  <TableCell className="text-xs">{[review.stage, review.step].filter(Boolean).join(' · ') || '–'}</TableCell>
                  <TableCell className="text-xs">{reviewersText(review) || '–'}</TableCell>
                  <TableCell className="text-xs max-md:hidden">{review.verifier ? [review.verifier.model, review.verifier.effort].filter(Boolean).join(', ') : '–'}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </Section>
  )
}

function Findings({ findings }: { findings: RecordFinding[] }) {
  return (
    <Section title="Findings" name="findings">
      {findings.length === 0 ? <p className="text-sm text-muted-foreground">No findings.</p> : (
        <div className="overflow-x-auto border">
          <Table aria-label="Findings">
            <TableHeader>
              <TableRow>
                <TableHead>ID</TableHead>
                <TableHead>Found by</TableHead>
                <TableHead className="max-md:hidden">Severity</TableHead>
                <TableHead>Finding</TableHead>
                <TableHead>Decisions</TableHead>
                <TableHead>Outcome</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {findings.map((finding, i) => <FindingRow key={`${i}:${finding.id}`} finding={finding} />)}
            </TableBody>
          </Table>
        </div>
      )}
    </Section>
  )
}

function FindingRow({ finding }: { finding: RecordFinding }) {
  const decisions = decisionsOf(finding)
  const words = (finding.events ?? []).map(e => ({ type: e.type, words: eventWords(e) })).filter(e => e.words.length > 0)
  const place = finding.file ? `${finding.file}${finding.line != null ? `:${finding.line}` : ''}` : null
  return (
    <TableRow data-finding={finding.id} className="align-top">
      <TableCell className="font-mono text-xs whitespace-nowrap">{finding.id}</TableCell>
      <TableCell className="text-xs" data-cell="found-by">
        {foundByText(finding)}
        {missedByTheReviews(finding) && <Badge variant="warn" className="ml-1.5">missed</Badge>}
      </TableCell>
      <TableCell className="text-xs max-md:hidden">{finding.severity ?? '–'}</TableCell>
      <TableCell className="min-w-64 max-w-xl text-xs">
        <div className="font-medium">{finding.title ?? 'No title'}</div>
        {place && <div className="font-mono text-muted-foreground break-all">{place}</div>}
        {(finding.scenario || words.length > 0) && (
          <details className="mt-1">
            <summary className="cursor-pointer text-muted-foreground">Details</summary>
            {finding.scenario && <p className="mt-1 whitespace-pre-wrap">{finding.scenario}</p>}
            {words.length > 0 && (
              <ul className="mt-1 space-y-0.5">
                {words.map((e, i) => <li key={i}><span className="text-muted-foreground">{e.type}:</span> {e.words.join(' · ')}</li>)}
              </ul>
            )}
          </details>
        )}
      </TableCell>
      <TableCell className="text-xs" data-cell="decisions">{decisions.length ? decisions.join(', ') : <span className="text-muted-foreground">None</span>}</TableCell>
      <TableCell data-cell="outcome">
        <Badge variant={OUTCOME_BADGES[finding.outcome ?? ''] ?? 'neutral'}>{finding.outcome ?? 'unknown'}</Badge>
        {finding.confidence === 'low' && <div className="mt-0.5 text-xs text-muted-foreground">low confidence</div>}
      </TableCell>
    </TableRow>
  )
}

function Confirmations({ record }: { record: ReviewRecord }) {
  const confirmations = record.confirmations ?? []
  return (
    <Section title="Confirmations" name="confirmations">
      {confirmations.length === 0 ? <p className="text-sm text-muted-foreground">No confirmations.</p> : (
        <div className="overflow-x-auto border">
          <Table aria-label="Confirmations">
            <TableHeader>
              <TableRow>
                <TableHead>ID</TableHead>
                <TableHead>Reviewer</TableHead>
                <TableHead>Ask</TableHead>
                <TableHead>Given</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {confirmations.map((c, i) => (
                <TableRow key={`${i}:${c.id}`} data-confirmation={c.id}>
                  <TableCell className="font-mono text-xs whitespace-nowrap">{c.id}</TableCell>
                  <TableCell className="text-xs">{c.reviewer ?? '–'}</TableCell>
                  <TableCell className="text-xs whitespace-pre-wrap">{c.ask ?? ''}</TableCell>
                  <TableCell>
                    <Badge variant={c.given === true ? 'ok' : c.given === false ? 'warn' : 'neutral'}>{givenText(c.given)}</Badge>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </Section>
  )
}

function Rounds({ rounds, shown, listPath }: { rounds: ReviewRound[]; shown: string; listPath: string }) {
  if (rounds.length < 2) return null
  return (
    <Section title="Rounds of this PR" name="rounds">
      <ul className="space-y-1 text-sm">
        {rounds.map(r => (
          <li key={r.id} data-round={r.round}>
            {r.id === shown
              ? <span className="font-medium">Round {r.round} (shown)</span>
              : <Link href={recordPath(listPath, { id: r.id })} className="underline underline-offset-2">Round {r.round}</Link>}
            <span className="ml-2 font-mono text-xs text-muted-foreground">{format(new Date(r.stored_at), 'MMM d, HH:mm')} · {r.session_id.slice(0, 8)}</span>
          </li>
        ))}
      </ul>
    </Section>
  )
}
