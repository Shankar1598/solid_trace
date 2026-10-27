import { router } from '@inertiajs/react'
import { TimeRangePicker } from '@/components/TimeRangePicker'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { performanceSearch, rangeOfAnswer, type AnsweredRange } from '@/lib/performance'

const ALL = '__all'

/**
 * The environment and range pickers of Performance and a Transaction's page.
 * Picking one visits `path` with both in its search, as the Logs explorer
 * keeps its range, and with `more`, such as the Transaction's name.
 */
export function PerformanceFilters({ path, range, environment, environments, more }: {
  path: string
  range: AnsweredRange
  environment: string | null
  environments: string[]
  more?: Record<string, string>
}) {
  const current = rangeOfAnswer(range)
  const listed = environment && !environments.includes(environment) ? [...environments, environment] : environments
  const items = [{ value: ALL, label: 'All environments' }, ...listed.map(e => ({ value: e, label: e }))]
  return (
    <div className="flex flex-wrap items-center gap-2">
      <Select
        value={environment ?? ALL}
        items={items}
        onValueChange={value => value && router.visit(`${path}${performanceSearch(current, value === ALL ? null : value, more)}`)}
      >
        <SelectTrigger aria-label="Environment"><SelectValue /></SelectTrigger>
        <SelectContent>
          {items.map(item => <SelectItem key={item.value} value={item.value}>{item.label}</SelectItem>)}
        </SelectContent>
      </Select>
      <TimeRangePicker range={current} onChange={next => router.visit(`${path}${performanceSearch(next, environment, more)}`)} />
    </div>
  )
}
