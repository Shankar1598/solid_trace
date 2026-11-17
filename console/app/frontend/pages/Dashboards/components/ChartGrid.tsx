import { useEffect, useRef, useState } from 'react'
import ReactGridLayout, { type Layout, type LayoutItem } from 'react-grid-layout'
import { toast } from 'sonner'
import { saveLayout } from '@/lib/dashboards/api'
import {
  GRID_COLUMNS, movedPlaces, stackedPlaces, type DashboardChart, type DashboardFilter, type FilterValues, type Place,
} from '@/lib/dashboards/dashboard'
import type { FetchQueue } from '@/lib/dashboards/fetchQueue'
import type { TimeRange } from '@/lib/timeRange'
import type { ProjectOption } from '../../Logs/components/BuilderPanel'
import { ChartCard, type DashboardRun } from './ChartCard'

/** A grid row's height and the gap between cards, in px. */
const ROW_PX = 64
const GAP_PX = 12

const placesOf = (charts: DashboardChart[]): Place[] => charts.map(({ id, x, y, w, h }) => ({ id, x, y, w, h }))
const layoutOf = (charts: DashboardChart[]): LayoutItem[] => charts.map(c => ({ i: String(c.id), x: c.x, y: c.y, w: c.w, h: c.h, minW: 2, minH: 2 }))

/**
 * The charts of one group of a Dashboard, the charts in no Section or those
 * of one Section, on a grid of their own: a chart's x and y are its place in
 * the group. While arranging, the cards are dragged by their titles and
 * resized from their corners, and each move saves the places that changed.
 * Stacked (a phone), the cards are one under another and never saved.
 */
export function ChartGrid({
  charts, width, stacked, arranging, orgSlug, dashboardId, projectsById, showProject, run, range, filters, values, queue,
  onRunning, onEdit, onDuplicate, onDelete,
}: {
  charts: DashboardChart[]
  width: number
  stacked: boolean
  arranging: boolean
  orgSlug: string
  dashboardId: number
  projectsById: Map<number, ProjectOption>
  showProject: boolean
  run: DashboardRun
  range: TimeRange
  filters: DashboardFilter[]
  values: FilterValues
  queue: FetchQueue
  onRunning: (id: number, running: boolean) => void
  onEdit: (chart: DashboardChart) => void
  onDuplicate: (chart: DashboardChart) => void
  onDelete: (chart: DashboardChart) => void
}) {
  // The grid's places, and the ones last saved, so each move saves only what changed.
  const [layout, setLayout] = useState<LayoutItem[]>(() => layoutOf(charts))
  const saved = useRef<Place[]>(placesOf(charts))
  useEffect(() => {
    setLayout(layoutOf(charts))
    saved.current = placesOf(charts)
  }, [charts])
  const persist = (next: Layout) => {
    setLayout([...next])
    const moved = movedPlaces(saved.current, [...next])
    if (moved.length === 0) return
    const before = saved.current
    saved.current = saved.current.map(p => moved.find(m => m.id === p.id) ?? p)
    saveLayout(orgSlug, dashboardId, moved).catch(error => {
      saved.current = before
      toast.error(`The layout wasn't saved: ${error.message}`)
    })
  }

  return (
    <ReactGridLayout
      layout={stacked ? stackedPlaces(layout) : layout}
      width={width}
      gridConfig={{ cols: GRID_COLUMNS, rowHeight: ROW_PX, margin: [GAP_PX, GAP_PX], containerPadding: [0, 0] }}
      dragConfig={{ enabled: arranging, handle: '.dashboard-chart-handle' }}
      resizeConfig={{ enabled: arranging }}
      onDragStop={persist}
      onResizeStop={persist}
    >
      {charts.map(chart => (
        <div key={String(chart.id)}>
          <ChartCard
            chart={chart}
            project={projectsById.get(chart.project_id)}
            showProject={showProject}
            orgSlug={orgSlug}
            run={run}
            range={range}
            filters={filters}
            values={values}
            queue={queue}
            editing={arranging}
            onRunning={onRunning}
            onEdit={() => onEdit(chart)}
            onDuplicate={() => onDuplicate(chart)}
            onDelete={() => onDelete(chart)}
          />
        </div>
      ))}
    </ReactGridLayout>
  )
}
