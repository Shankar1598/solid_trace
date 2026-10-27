import { Table, TableBody, TableCell, TableFooter, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { statText, valueText } from '@/lib/charts/axis'
import { bytesText, chartColor, type OverviewSpanCounts, type OverviewSpans as Spans } from '@/lib/overview'

/**
 * The Overview's Spans: each Project that has any, in creation order and
 * wearing its Chart colour, and the total.
 */
export function OverviewSpans({ spans }: { spans: Spans }) {
  return (
    <Table aria-label="Spans">
      <TableHeader>
        <TableRow>
          <TableHead>Project</TableHead>
          <TableHead className="text-right">Received</TableHead>
          <TableHead className="text-right">Received bytes</TableHead>
          <TableHead className="text-right">Stored</TableHead>
          <TableHead className="text-right">Stored bytes</TableHead>
          <TableHead className="text-right">Trace Retention</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {spans.projects.map(p => (
          <TableRow key={p.id} data-project={p.slug}>
            <TableCell>
              <span className="flex items-center gap-2">
                <span className="h-2 w-2 shrink-0" style={{ background: chartColor(p.color) }} aria-hidden />
                <span className="font-medium">{p.name}</span>
              </span>
            </TableCell>
            <Counts counts={p} />
            <TableCell className="text-right font-mono tabular-nums whitespace-nowrap">
              {p.retention_days} days{p.retention_is_default && <span className="text-muted-foreground"> (default)</span>}
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
      <TableFooter>
        <TableRow data-total>
          <TableCell className="font-medium">All Projects</TableCell>
          <Counts counts={spans} />
          <TableCell />
        </TableRow>
      </TableFooter>
    </Table>
  )
}

function Counts({ counts }: { counts: OverviewSpanCounts }) {
  return (
    <>
      <Num value={statText(counts.received)} exact={valueText(counts.received)} />
      <Num value={bytesText(counts.received_bytes)} />
      <Num value={statText(counts.stored)} exact={valueText(counts.stored)} />
      <Num value={bytesText(counts.stored_bytes)} />
    </>
  )
}

function Num({ value, exact }: { value: string; exact?: string }) {
  return <TableCell className="text-right font-mono tabular-nums whitespace-nowrap" title={exact}>{value}</TableCell>
}
