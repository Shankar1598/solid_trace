import { Badge } from '@/components/ui/badge'
import type { Monitor, MonitorStatus } from '@/lib/monitors/monitor'

const LABELS: Record<MonitorStatus, string> = { firing: 'Firing', ok: 'OK', disabled: 'Off', error: 'Error' }

/** A Monitor's status: firing (with how many groups), ok, off, or error when its last run failed. */
export function MonitorStatusBadge({ monitor }: { monitor: Pick<Monitor, 'status' | 'firing_count' | 'last_error'> }) {
  const { status } = monitor
  const label = status === 'firing' && monitor.firing_count > 1 ? `Firing (${monitor.firing_count})` : LABELS[status]
  return (
    <Badge
      variant={status === 'firing' || status === 'error' ? 'destructive' : status === 'ok' ? 'secondary' : 'outline'}
      title={status === 'error' ? monitor.last_error ?? undefined : undefined}
      data-monitor-status={status}
    >
      {label}
    </Badge>
  )
}
