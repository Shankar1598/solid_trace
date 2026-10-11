import { ChartArea, ChartColumn, ChartLine, Hash, Table2 } from 'lucide-react'
import type { ChartForm } from '@/lib/charts/series'
import { cn } from '@/lib/utils'

/** A form a table can be shown in: drawn as a chart over the table, or the table alone. */
export type Shown = ChartForm | 'table'

const OPTIONS: Record<Shown, { label: string; icon: typeof ChartLine }> = {
  line: { label: 'Line', icon: ChartLine },
  area: { label: 'Area', icon: ChartArea },
  bars: { label: 'Bars', icon: ChartColumn },
  stat: { label: 'Number', icon: Hash },
  table: { label: 'Table', icon: Table2 },
}

/**
 * The `Line | Area | Bars | Table` switch over a table that can be drawn: the
 * forms it can take, then Table. Pressed buttons in a group, as the Mode
 * switch is, so each is a tab stop that Enter or Space presses.
 */
export function ChartSwitch({ forms, shown, onChange }: { forms: ChartForm[]; shown: Shown; onChange: (shown: Shown) => void }) {
  return (
    <div role="group" aria-label="Chart" className="flex shrink-0 overflow-hidden rounded-md border text-[11px]">
      {[...forms, 'table' as const].map(form => {
        const { label, icon: Icon } = OPTIONS[form]
        const on = form === shown
        return (
          <button
            key={form}
            type="button"
            aria-pressed={on}
            onClick={() => !on && onChange(form)}
            className={cn(
              'flex h-6 items-center gap-1 border-l px-2 first:border-l-0 focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-ring',
              on ? 'bg-accent font-medium text-accent-foreground' : 'text-muted-foreground hover:bg-muted hover:text-foreground',
            )}
          >
            <Icon className="h-3 w-3" />
            {label}
          </button>
        )
      })}
    </div>
  )
}
