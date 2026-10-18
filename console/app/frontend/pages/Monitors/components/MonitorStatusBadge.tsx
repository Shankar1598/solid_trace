import { Badge } from '@/components/ui/badge'
import type { Monitor, MonitorStatus } from '@/lib/monitors/monitor'

const LABELS: Record<MonitorStatus, string> = { firing: 'Firing', ok: 'OK', disabled: 'Off', error: 'Error' }
const TONES = { firing: 'error', ok: 'ok', disabled: 'neutral', error: 'warn' } as const

/**
 * A Monitor's status: firing (with how many groups), ok, off, or error when its last run failed.
 * A firing Monitor whose last run failed is still firing, with the failure noted beside it.
 */
export function MonitorStatusBadge({ monitor }: { monitor: Pick<Monitor, 'status' | 'firing_count' | 'last_error'> }) {
  const { status } = monitor
  const label = status === 'firing' && monitor.firing_count > 1 ? `Firing (${monitor.firing_count})` : LABELS[status]
  const failed = status === 'firing' && monitor.last_error
  return (
    <span className="inline-flex items-center gap-2">
      <Badge
        variant={TONES[status]}
        dot
        title={status === 'error' || failed ? monitor.last_error ?? undefined : undefined}
        data-monitor-status={status}
      >
        {label}
      </Badge>
      {failed && (
        <span className="text-xs text-muted-foreground" title={monitor.last_error ?? undefined}>Last run failed</span>
      )}
    </span>
  )
}
