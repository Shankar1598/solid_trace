import { Plus, Trash2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Segmented } from '@/components/ui/segmented'
import { MAX_THRESHOLDS, type ThresholdRow } from '@/lib/dashboards/dashboard'

const SEVERITIES: { value: ThresholdRow['severity']; label: string }[] = [
  { value: '', label: 'None' },
  { value: 'warn', label: 'Warn' },
  { value: 'critical', label: 'Critical' },
]
/** The longest label a threshold line has, as the Dashboard Chart model allows. */
const MAX_LABEL = 40

/**
 * The chart editor's threshold lines of a line or area chart: a row for
 * each, of its value, label and severity, which a member adds and removes,
 * at most MAX_THRESHOLDS.
 */
export function ThresholdsField({ rows, onChange }: { rows: ThresholdRow[]; onChange: (rows: ThresholdRow[]) => void }) {
  const change = (index: number, changed: Partial<ThresholdRow>) => onChange(rows.map((row, i) => (i === index ? { ...row, ...changed } : row)))
  return (
    <div className="space-y-1" data-chart-thresholds>
      <Label>Threshold lines</Label>
      {rows.map((row, index) => {
        const n = index + 1
        return (
          <div key={index} className="flex flex-wrap items-center gap-2" data-threshold-row>
            <Input className="h-7 w-24" type="number" step="any" value={row.value} placeholder="Value" aria-label={`Threshold ${n} value`}
              onChange={e => change(index, { value: e.target.value })} />
            <Input className="h-7 w-40" value={row.label} placeholder="Label (optional)" maxLength={MAX_LABEL} aria-label={`Threshold ${n} label`}
              onChange={e => change(index, { label: e.target.value })} />
            <Segmented size="sm" label={`Threshold ${n} severity`} value={row.severity} options={SEVERITIES} onChange={severity => change(index, { severity })} />
            <Button type="button" variant="ghost" size="icon" className="h-7 w-7" aria-label={`Remove threshold ${n}`} title={`Remove threshold ${n}`}
              onClick={() => onChange(rows.filter((_, i) => i !== index))}><Trash2 className="h-3.5 w-3.5" /></Button>
          </div>
        )
      })}
      <Button type="button" variant="outline" size="sm" disabled={rows.length >= MAX_THRESHOLDS}
        onClick={() => onChange([...rows, { value: '', label: '', severity: '' }])}>
        <Plus className="h-3.5 w-3.5" /> Add threshold
      </Button>
    </div>
  )
}
