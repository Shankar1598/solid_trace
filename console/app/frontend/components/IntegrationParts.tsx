import { formatDistanceToNow } from 'date-fns'
import { TriangleAlert } from 'lucide-react'
import type { IntegrationFailure } from '@/types'

/** The Factory provider's connection: it has no URL field, because Console sends only to the Dispatcher. */
export function FactoryConnection() {
  return (
    <div className="space-y-2 pt-6 border-t" data-testid="factory-connection">
      <h3 className="text-sm font-semibold">Connection</h3>
      <p className="text-sm text-muted-foreground">
        The Factory has no URL. Console sends to the Dispatcher of Settings → Instance, through a Webhook sender that it
        makes at the Dispatcher when you add this Integration, and deletes with it. Each Issue that a rule below
        applies to, and each Monitor that names this Integration, starts a Run in the Project of the Issue or the
        Monitor, or gives the Run that is already active for it. A Monitor&apos;s resolve starts no Run.
      </p>
    </div>
  )
}

/**
 * The Integration's last delivery that failed, with the reason. A sent
 * delivery after it hides it, but Console keeps sent rows for 1 day only,
 * so the text does not say that this delivery was the last one.
 */
export function LastFailure({ failure }: { failure: IntegrationFailure }) {
  return (
    <div
      className="flex items-start gap-2 border border-destructive/40 p-3 text-sm"
      role="alert"
      data-testid="integration-last-failure"
    >
      <TriangleAlert className="mt-0.5 size-4 shrink-0 text-destructive" aria-hidden />
      <div className="space-y-1">
        <p className="font-medium">
          A delivery failed {formatDistanceToNow(new Date(failure.at))} ago
          {failure.retried ? '. Console tries it again.' : ', and Console does not try it again.'}
        </p>
        <p className="font-mono text-xs text-muted-foreground break-all">{failure.reason}</p>
      </div>
    </div>
  )
}
