import type { ReactNode } from 'react'
import { cn } from '@/lib/utils'

/**
 * One part of a form: its title, a line about it, then its fields. A rule
 * divides it from the section before, so a form reads as a column of steps.
 * `actions` sit at the right of the title: a switch or a refresh.
 */
export function FormSection({ title, description, actions, children, className }: {
  title: ReactNode
  description?: ReactNode
  actions?: ReactNode
  children: ReactNode
  className?: string
}) {
  return (
    <section className={cn('space-y-4 border-t pt-6', className)}>
      <div className="flex flex-wrap items-start justify-between gap-x-4 gap-y-2">
        <div className="min-w-0 space-y-1">
          <h2 className="text-sm font-semibold">{title}</h2>
          {description && <p className="text-xs text-muted-foreground">{description}</p>}
        </div>
        {actions && <div className="flex flex-wrap items-center gap-2">{actions}</div>}
      </div>
      {children}
    </section>
  )
}
