import { Link, router } from '@inertiajs/react'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { statText, valueText } from '@/lib/charts/axis'
import { bytesText, chartColor, shareText, type Overview, type OverviewSource } from '@/lib/overview'

type Sources = NonNullable<Overview['sources']>

/**
 * The biggest sources: the Services and files that sent the most Logs and
 * those that kept the most bytes, side by side on wide screens. A source
 * opens the Logs explorer on its Project, filtered to its Service and file,
 * over the range's days.
 */
export function OverviewSources({ sources, range, orgSlug }: { sources: Sources; range: Overview['range']; orgSlug: string }) {
  if (sources.by_logs.length === 0) {
    return <p className="text-sm text-muted-foreground" data-empty>No Logs kept in this range yet.</p>
  }
  const explorer = (s: OverviewSource) => explorerPath(orgSlug, s, range)
  return (
    <div className="grid gap-8 lg:grid-cols-2">
      <SourceTable
        label="By Logs"
        ranked="by-logs"
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
        sources={sources.by_bytes}
        explorer={explorer}
        measure={s => <span title={`${valueText(s.bytes)} bytes`}>{bytesText(s.bytes)}</span>}
        measureName="Kept"
        shareOf="Of the bytes kept"
      />
    </div>
  )
}

function SourceTable({ label, ranked, sources, explorer, measure, measureName, shareOf }: {
  label: string
  ranked: string
  sources: OverviewSource[]
  explorer: (s: OverviewSource) => string
  measure: (s: OverviewSource) => React.ReactNode
  measureName: string
  shareOf: string
}) {
  return (
    <div className="space-y-2">
      <h3 className="text-sm font-medium">{label}</h3>
      <div className="border">
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
                <TableCell className="max-w-0 w-full">
                  <Link href={explorer(s)} className="block truncate font-medium hover:underline" title={sourceTitle(s)}>
                    {s.service || <span className="text-muted-foreground">No Service</span>}
                    {s.file && <span className="font-normal text-muted-foreground"> · {s.file}</span>}
                  </Link>
                  <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
                    <span className="h-2 w-2 shrink-0" style={{ background: chartColor(s.project.color) }} aria-hidden />
                    <span className="truncate">{s.project.name}</span>
                  </span>
                </TableCell>
                <TableCell className="text-right font-mono tabular-nums whitespace-nowrap">{measure(s)}</TableCell>
                <TableCell className="text-right font-mono tabular-nums whitespace-nowrap">{s.share === null ? '—' : shareText(s.share)}</TableCell>
                <TableCell className="text-right font-mono tabular-nums whitespace-nowrap" title={`${valueText(s.dropped_by_transform)} Logs`}>
                  {s.dropped_by_transform > 0 ? statText(s.dropped_by_transform) : <span className="text-muted-foreground">0</span>}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
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
