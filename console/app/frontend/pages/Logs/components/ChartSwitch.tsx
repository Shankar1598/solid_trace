import { ChartArea, ChartColumn, ChartLine, Hash, Table2 } from 'lucide-react'
import { Segmented } from '@/components/ui/segmented'
import type { ChartForm } from '@/lib/charts/series'

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
    <Segmented
      label="Chart"
      size="sm"
      value={shown}
      options={[...forms, 'table' as const].map(form => ({ value: form, ...OPTIONS[form] }))}
      onChange={onChange}
    />
  )
}
