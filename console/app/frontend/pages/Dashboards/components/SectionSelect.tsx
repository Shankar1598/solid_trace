import { useMemo } from 'react'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import type { DashboardSection } from '@/lib/dashboards/dashboard'

/** The select's value for no Section. */
const NO_SECTION = 'none'

/**
 * The editors' select of the Section a chart or a Text block is in: No
 * section, then the Dashboard's Sections in order, with the save's error
 * under it. The editors show it only when the Dashboard has Sections.
 */
export function SectionSelect({ sections, value, error, onChange }: {
  sections: DashboardSection[]
  /** The Section chosen, or null for none. */
  value: number | null
  /** The save's error for the Section, if any. */
  error?: string
  onChange: (sectionId: number | null) => void
}) {
  const items = useMemo(
    () => [{ value: NO_SECTION, label: 'No section' }, ...sections.map(s => ({ value: String(s.id), label: s.title }))],
    [sections],
  )
  return (
    <div className="space-y-1">
      <Label>Section</Label>
      <Select
        value={value === null ? NO_SECTION : String(value)}
        onValueChange={picked => picked && onChange(picked === NO_SECTION ? null : Number(picked))}
        items={items}
      >
        <SelectTrigger aria-label="Section" className="min-w-44">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {items.map(s => <SelectItem key={s.value} value={s.value}>{s.label}</SelectItem>)}
        </SelectContent>
      </Select>
      {error && <p className="text-xs text-destructive">Section {error}</p>}
    </div>
  )
}
