import type { Mode } from '@/lib/logs/explorerState'
import { cn } from '@/lib/utils'

const LABELS: Record<Mode, string> = { builder: 'Builder', query: 'Query' }

/** The `Builder | Query` switch in the bar. */
export function ModeSwitch({ modes, mode, disabled, onSwitch }: {
  modes: Mode[]; mode: Mode; disabled?: boolean; onSwitch: (mode: Mode) => void
}) {
  return (
    <div className="flex overflow-hidden rounded-md border text-xs font-medium" role="group" aria-label="Mode">
      {modes.map(m => (
        <button
          key={m}
          aria-pressed={m === mode}
          disabled={disabled}
          onClick={() => m !== mode && onSwitch(m)}
          className={cn('border-r px-2.5 py-1.5 last:border-0', m === mode ? 'bg-primary text-primary-foreground' : 'hover:bg-muted')}
        >
          {LABELS[m]}
        </button>
      ))}
    </div>
  )
}
