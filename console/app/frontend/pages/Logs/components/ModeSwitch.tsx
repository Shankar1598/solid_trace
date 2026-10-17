import { Segmented } from '@/components/ui/segmented'
import type { Mode } from '@/lib/logs/explorerState'

const LABELS: Record<Mode, string> = { builder: 'Builder', query: 'Query', sql: 'SQL' }

/** The `Builder | Query | SQL` switch in the bar (`Builder | Query` while SQL queries are off). */
export function ModeSwitch({ modes, mode, disabled, onSwitch }: {
  modes: Mode[]; mode: Mode; disabled?: boolean; onSwitch: (mode: Mode) => void
}) {
  return <Segmented label="Mode" value={mode} options={modes.map(m => ({ value: m, label: LABELS[m] }))} onChange={onSwitch} disabled={disabled} />
}
