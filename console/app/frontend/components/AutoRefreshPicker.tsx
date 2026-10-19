import { useEffect, useRef } from 'react'
import { RefreshCw } from 'lucide-react'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { AUTO_REFRESHES, AUTO_REFRESH_MS, type AutoRefresh } from '@/lib/autoRefresh'

const LABELS: Record<AutoRefresh, string> = { off: 'Off', '15s': '15s', '30s': '30s', '1m': '1m', '5m': '5m' }
const ITEMS = AUTO_REFRESHES.map(value => ({ value, label: LABELS[value] }))

/** Picks how often the page runs again; `paused` says why it isn't now, when on. */
export function AutoRefreshPicker({ value, onChange, paused }: { value: AutoRefresh; onChange: (value: AutoRefresh) => void; paused?: string | null }) {
  const title = value === 'off' ? 'Auto-refresh is off' : paused ? `Auto-refresh paused: ${paused}` : `Auto-refresh every ${LABELS[value]}`
  return (
    <Select value={value} onValueChange={next => next && onChange(next as AutoRefresh)} items={ITEMS}>
      <SelectTrigger size="sm" aria-label="Auto-refresh" title={title} className="h-8">
        <RefreshCw className={value === 'off' || paused ? 'h-3.5 w-3.5 text-muted-foreground' : 'h-3.5 w-3.5'} />
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {ITEMS.map(item => <SelectItem key={item.value} value={item.value}>{item.label}</SelectItem>)}
      </SelectContent>
    </Select>
  )
}

/**
 * Calls `tick` every `every` while `active`, skipping ticks while the tab is
 * hidden: a page nobody sees doesn't need its answers again.
 */
export function useAutoRefresh(every: AutoRefresh, active: boolean, tick: () => void) {
  const latest = useRef(tick)
  latest.current = tick
  useEffect(() => {
    const ms = AUTO_REFRESH_MS[every]
    if (!ms || !active) return
    const timer = setInterval(() => {
      if (!document.hidden) latest.current()
    }, ms)
    return () => clearInterval(timer)
  }, [every, active])
}
