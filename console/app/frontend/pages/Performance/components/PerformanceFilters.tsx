import { router, usePage } from '@inertiajs/react'
import { TimeRangePicker } from '@/components/TimeRangePicker'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { performancePath, performanceSearch, rangeOfAnswer, type AnsweredRange } from '@/lib/performance'
import type { SharedProps } from '@/types'

const ALL = '__all'

/**
 * The filter row of Performance and a Transaction's page, as Issues has one:
 * the Project and environment joined, then the range. Picking one visits
 * `path` with both in its search, as the Logs explorer keeps its range, and
 * with `more`, such as the Transaction's name. `project` shows the Project
 * picker, which opens that Project's Performance in the same range.
 */
export function PerformanceFilters({ path, project, range, environment, environments, more }: {
  path: string
  project?: string
  range: AnsweredRange
  environment: string | null
  environments: string[]
  more?: Record<string, string>
}) {
  const { current_org, org_projects } = usePage<SharedProps>().props
  const current = rangeOfAnswer(range)
  const listed = environment && !environments.includes(environment) ? [...environments, environment] : environments
  const items = [{ value: ALL, label: 'All environments' }, ...listed.map(e => ({ value: e, label: e }))]
  const projects = Object.fromEntries((org_projects ?? []).map(p => [p.slug, p.name]))
  return (
    <div className="flex flex-wrap items-center gap-2">
      <div className="flex items-center border">
        {project && (
          <Select
            value={project}
            items={projects}
            onValueChange={slug => slug && router.visit(`${performancePath(current_org?.slug ?? '', slug)}${performanceSearch(current, null)}`)}
          >
            <SelectTrigger aria-label="Project" className="border-0 border-r"><SelectValue /></SelectTrigger>
            <SelectContent>
              {Object.entries(projects).map(([slug, name]) => <SelectItem key={slug} value={slug}>{name}</SelectItem>)}
            </SelectContent>
          </Select>
        )}
        <Select
          value={environment ?? ALL}
          items={items}
          onValueChange={value => value && router.visit(`${path}${performanceSearch(current, value === ALL ? null : value, more)}`)}
        >
          <SelectTrigger aria-label="Environment" className="border-0"><SelectValue /></SelectTrigger>
          <SelectContent>
            {items.map(item => <SelectItem key={item.value} value={item.value}>{item.label}</SelectItem>)}
          </SelectContent>
        </Select>
      </div>
      <TimeRangePicker range={current} onChange={next => router.visit(`${path}${performanceSearch(next, environment, more)}`)} />
    </div>
  )
}
