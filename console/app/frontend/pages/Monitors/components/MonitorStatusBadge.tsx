import { Badge } from '@/components/ui/badge'
import { firingNote, type Monitor, type MonitorStatus } from '@/lib/monitors/monitor'

const LABELS: Record<MonitorStatus, string> = { firing: 'Firing', ok: 'OK', disabled: 'Off', held: 'Held', error: 'Error' }
const TONES = { firing: 'error', ok: 'ok', disabled: 'neutral', held: 'neutral', error: 'warn' } as const

/**
 * A Monitor's status: firing (with how many groups), ok, off, held while its runs wait for complete Logs,
 * or error when its last run failed. A firing Monitor whose last run failed or was held is still firing,
 * with that noted beside it.
 */
export function MonitorStatusBadge({ monitor }: { monitor: Pick<Monitor, 'status' | 'firing_count' | 'last_error' | 'held_since'> }) {
  const { status } = monitor
  const label = status === 'firing' && monitor.firing_count > 1 ? `Firing (${monitor.firing_count})` : LABELS[status]
  const note = firingNote(monitor)
  return (
    <span className="inline-flex items-center gap-2">
      <Badge
        variant={TONES[status]}
        dot
        title={status === 'error' || status === 'firing' ? monitor.last_error ?? undefined : undefined}
        data-monitor-status={status}
      >
        {label}
      </Badge>
      {note && <span className="text-xs text-muted-foreground" title={monitor.last_error ?? undefined}>{note}</span>}
    </span>
  )
}
