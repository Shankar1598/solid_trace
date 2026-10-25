import { Link, router, usePage } from '@inertiajs/react'
import { AlertTriangle, ScrollText } from 'lucide-react'
import DashboardLayout from '@/components/layouts/DashboardLayout'
import { PageHeader } from '@/components/PageHeader'
import { useAutoRefresh } from '@/components/AutoRefreshPicker'
import { Segmented } from '@/components/ui/segmented'
import { Card, CardContent } from '@/components/ui/card'
import { OverviewSection } from '@/components/overview/OverviewSection'
import { OverviewBand } from '@/components/overview/OverviewBand'
import { OverviewRates } from '@/components/overview/OverviewRates'
import { OverviewDailyCharts } from '@/components/overview/OverviewDailyCharts'
import { OverviewProjects } from '@/components/overview/OverviewProjects'
import { OverviewSources } from '@/components/overview/OverviewSources'
import { OverviewHealth } from '@/components/overview/OverviewHealth'
import { OverviewStorage } from '@/components/overview/OverviewStorage'
import { OVERVIEW_RANGES, dayText, secondText, type Overview, type OverviewRangeName } from '@/lib/overview'
import type { SharedProps } from '@/types'

/**
 * An Organization's Overview: what came in and what was kept, over whole
 * UTC days, laid out as a dashboard of panels that fits about one view on a
 * wide screen. Its sections come from Console's Overview builder; each is
 * its own component under components/overview, in the order below.
 * Compared with other tools is left off the page for now; get_overview
 * still answers it. Today refreshes every minute while the tab is visible.
 */
export default function OverviewShow({ overview }: { overview: Overview }) {
  const orgSlug = usePage<SharedProps>().props.current_org?.slug ?? ''
  const { range, band, rates } = overview
  const pick = (name: OverviewRangeName) =>
    router.get(`/${orgSlug}/overview`, name === 'today' ? {} : { range: name }, { preserveScroll: true })
  useAutoRefresh('1m', range.name === 'today', () => router.reload({ only: ['overview'] }))

  return (
    <DashboardLayout>
      <div className="mx-auto max-w-[1600px]">
        <PageHeader
          className="mb-4"
          title="Overview"
          description={`What came in and what was kept, ${rangeText(overview)}. Days are UTC days.`}
          actions={<Segmented label="Range" value={range.name} options={OVERVIEW_RANGES} onChange={pick} />}
        />

        {overview.empty.projects ? (
          <EmptyState title="No Projects yet" orgSlug={orgSlug}>
            Make a Project and send it Logs, and this page shows how much arrives and what it takes to keep.
          </EmptyState>
        ) : overview.empty.logs ? (
          <EmptyState title="No Logs yet" orgSlug={orgSlug}>
            None of this Organization's Projects has Logs yet. Open a Project to see how to send them.
          </EmptyState>
        ) : null}

        {!overview.event_store.available && (
          <div role="alert" className="mb-3 flex items-start gap-2 border border-warning/40 px-3 py-2 text-sm">
            <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" aria-hidden />
            <span>{overview.event_store.error}</span>
          </div>
        )}

        <div className="grid grid-cols-1 gap-3 xl:grid-cols-12">
          {band && !overview.empty.logs && (
            <OverviewSection
              id="band"
              className="xl:col-span-8"
              title="Received → Kept → Stored"
              description="Received is what shippers sent, by second of arrival. Kept is what Log processing stored of the range's days, and Stored what ClickHouse holds of them, both by the Logs' own time."
            >
              {overview.ingest_counts?.starts_after_range && (
                <p className="mb-2 text-xs text-muted-foreground" data-notice="counts-start">
                  {overview.ingest_counts.since
                    ? `Ingest counts begin ${secondText(overview.ingest_counts.since)}, after this range starts, so Received can be shorter than Stored.`
                    : 'Nothing has been counted on arrival yet, so Received is empty.'}
                </p>
              )}
              {overview.source_counts?.starts_after_range && (
                <p className="mb-2 text-xs text-muted-foreground" data-notice="source-counts-start">
                  {overview.source_counts.since
                    ? `Source counts begin ${dayText(overview.source_counts.since)}, after this range starts, so Kept covers only the days since and the ratio compares those days alone.`
                    : 'Nothing kept has been counted yet, so Kept is empty and there is no ratio.'}
                </p>
              )}
              <OverviewBand band={band} />
            </OverviewSection>
          )}

          {rates && !overview.empty.logs && (
            <OverviewSection id="rates" className="xl:col-span-4" title="Rates" description="By arrival time. The peak is the busiest second of all the Projects together.">
              <OverviewRates rates={rates} />
            </OverviewSection>
          )}

          {overview.daily && !overview.empty.logs && (
            <OverviewSection id="daily" className="xl:col-span-12" bodyClassName="" title="By day" description="Received is by day of arrival, stored by the Logs' own day.">
              <OverviewDailyCharts daily={overview.daily} />
            </OverviewSection>
          )}

          {overview.projects && !overview.empty.logs && (
            <OverviewSection
              id="projects"
              className="xl:col-span-12"
              bodyClassName=""
              title="Projects"
              description="The range's Logs and bytes, and what each Project keeps of every day. Open a row to see its Logs."
            >
              <OverviewProjects projects={overview.projects} orgSlug={orgSlug} />
            </OverviewSection>
          )}

          {overview.sources && !overview.empty.logs && (
            <OverviewSection
              id="sources"
              className="xl:col-span-7 xl:h-96"
              bodyClassName="xl:overflow-y-auto"
              title="Biggest sources"
              description="The Services and files that sent the most Logs and kept the most bytes, by the Logs' own day. Open one to see its Logs."
            >
              <OverviewSources sources={overview.sources} range={range} orgSlug={orgSlug} />
            </OverviewSection>
          )}

          {!overview.empty.projects && (
            <OverviewSection
              id="health"
              className="xl:col-span-5 xl:h-96"
              bodyClassName="p-3 xl:overflow-y-auto"
              title="Health"
              description="How long Log processing keeps Logs waiting and how fast reads ran over the last 24 hours, Monitors that need a look, and what's new in Issues."
            >
              <OverviewHealth health={overview.health} orgSlug={orgSlug} />
            </OverviewSection>
          )}

          {overview.storage && (
            <OverviewSection
              id="storage"
              className="xl:col-span-12"
              title="Storage of the whole install"
              description="Every Organization's Logs on this install, not only this one's. Only Instance admins see this section."
            >
              <OverviewStorage storage={overview.storage} />
            </OverviewSection>
          )}
        </div>
      </div>
    </DashboardLayout>
  )
}

function rangeText({ range }: Overview): string {
  if (range.name === 'today') return `today, ${range.to}`
  return `the last ${range.days} days, ${range.from} to ${range.to}`
}

function EmptyState({ title, orgSlug, children }: { title: string; orgSlug: string; children: React.ReactNode }) {
  return (
    <Card className="mb-3 border-dashed">
      <CardContent className="flex flex-col items-center justify-center py-12 text-center">
        <ScrollText className="mb-4 h-12 w-12 text-muted-foreground opacity-50" aria-hidden />
        <h2 className="text-lg font-medium">{title}</h2>
        <p className="mb-4 max-w-md text-sm text-muted-foreground">{children}</p>
        <Link href={`/${orgSlug}/projects`} className="text-sm font-medium underline underline-offset-2">Go to Projects</Link>
      </CardContent>
    </Card>
  )
}
