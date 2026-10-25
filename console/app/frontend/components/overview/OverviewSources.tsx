import { useState } from 'react'
import { Link, router } from '@inertiajs/react'
import { Segmented } from '@/components/ui/segmented'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { statText, valueText } from '@/lib/charts/axis'
import { bytesText, chartColor, shareText, type Overview, type OverviewSource } from '@/lib/overview'

type Sources = NonNullable<Overview['sources']>

/**
 * The biggest sources: the Services and files that sent the most Logs and
 * those that kept the most bytes, one ranking at a time under a switch, so
 * the panel stays narrow enough to share a row. The other ranking stays in
 * the page, hidden. A source opens the Logs explorer on its Project,
 * filtered to its Service and file, over the range's days.
 */
export function OverviewSources({ sources, range, orgSlug }: { sources: Sources; range: Overview['range']; orgSlug: string }) {
  if (sources.by_logs.length === 0) {
    return <p className="p-3 text-sm text-muted-foreground" data-empty>No Logs kept in this range yet.</p>
  }
  return <SourceRankings sources={sources} explorer={(s: OverviewSource) => explorerPath(orgSlug, s, range)} />
}

type Ranking = 'by-logs' | 'by-bytes'
const RANKINGS: { value: Ranking; label: string }[] = [
  { value: 'by-logs', label: 'By Logs' },
  { value: 'by-bytes', label: 'By bytes' },
]

function SourceRankings({ sources, explorer }: { sources: Sources; explorer: (s: OverviewSource) => string }) {
  const [ranking, setRanking] = useState<Ranking>('by-logs')
  return (
    <div>
      <div className="px-3 pt-2">
        <Segmented label="Rank sources" size="sm" value={ranking} options={RANKINGS} onChange={setRanking} />
      </div>
      <SourceTable
        label="By Logs"
        ranked="by-logs"
        hidden={ranking !== 'by-logs'}
        sources={sources.by_logs}
        explorer={explorer}
        measure={s => (
          <span title={`${valueText(s.sent)} Logs sent, ${valueText(s.logs)} kept`}>{statText(s.sent)}</span>
        )}
        measureName="Logs"
        shareOf="Of the Logs sent, kept or dropped by the Log transform"
      />
      <SourceTable
        label="By bytes"
        ranked="by-bytes"
        hidden={ranking !== 'by-bytes'}
        sources={sources.by_bytes}
        explorer={explorer}
        measure={s => <span title={`${valueText(s.bytes)} bytes`}>{bytesText(s.bytes)}</span>}
        measureName="Kept"
        shareOf="Of the bytes kept"
      />
    </div>
  )
}

function SourceTable({ label, ranked, hidden, sources, explorer, measure, measureName, shareOf }: {
  label: string
  ranked: string
  hidden: boolean
  sources: OverviewSource[]
  explorer: (s: OverviewSource) => string
  measure: (s: OverviewSource) => React.ReactNode
  measureName: string
  shareOf: string
}) {
  return (
    <div className="min-w-0" hidden={hidden}>
      <Table aria-label={`Biggest sources ${label.toLowerCase()}`} data-sources={ranked}>
        <TableHeader>
          <TableRow>
            <TableHead>Source</TableHead>
            <TableHead className="text-right">{measureName}</TableHead>
            <TableHead className="text-right" title={shareOf}>Share</TableHead>
            <TableHead className="text-right" title="Logs the Log transform dropped">Dropped</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {sources.map(s => (
            <TableRow
              key={`${s.project.id}/${s.service}/${s.file}`}
              data-source={`${s.project.slug}/${s.service}/${s.file}`}
              className="cursor-pointer"
              onClick={event => {
                if ((event.target as HTMLElement).closest('a')) return
                router.visit(explorer(s))
              }}
            >
              <TableCell className="max-w-0 w-full py-1.5">
                <span className="flex min-w-0 items-center gap-1.5">
                  <span className="h-2 w-2 shrink-0" style={{ background: chartColor(s.project.color) }} aria-hidden />
                  <Link href={explorer(s)} className="min-w-0 truncate font-medium hover:underline" title={sourceTitle(s)}>
                    {s.service || <span className="text-muted-foreground">No Service</span>}
                    {s.file && <span className="font-normal text-muted-foreground"> · {s.file}</span>}
                  </Link>
                  <span className="ml-auto min-w-0 max-w-[40%] shrink truncate text-xs text-muted-foreground">{s.project.name}</span>
                </span>
              </TableCell>
              <TableCell className="py-1.5 text-right font-mono tabular-nums whitespace-nowrap">{measure(s)}</TableCell>
              <TableCell className="py-1.5 text-right font-mono tabular-nums whitespace-nowrap">{s.share === null ? '—' : shareText(s.share)}</TableCell>
              <TableCell className="py-1.5 text-right font-mono tabular-nums whitespace-nowrap" title={`${valueText(s.dropped_by_transform)} Logs`}>
                {s.dropped_by_transform > 0 ? statText(s.dropped_by_transform) : <span className="text-muted-foreground">0</span>}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}

function sourceTitle(s: OverviewSource): string {
  return `${s.service || 'No Service'}${s.file ? ` · ${s.file}` : ''} in ${s.project.name}`
}

/**
 * The Logs explorer on a source's Project, filtered to its Service and file
 * (isempty for a source without one, as a test of a missing Field is
 * false), over the range's whole UTC days.
 */
export function explorerPath(orgSlug: string, s: OverviewSource, range: Overview['range']): string {
  const from = new Date(`${range.from}T00:00:00Z`)
  const to = new Date(`${range.to}T00:00:00Z`)
  to.setUTCDate(to.getUTCDate() + 1)
  const is = (field: string, value: string) => (value === '' ? `where isempty(${field})` : `where ${field} == ${JSON.stringify(value)}`)
  const params = new URLSearchParams({
    from: from.toISOString(),
    to: to.toISOString(),
    mode: 'builder',
    q: `${is('service', s.service)}\n| ${is('file', s.file)}`,
  })
  return `/${orgSlug}/projects/${s.project.slug}/logs?${params}`
}
