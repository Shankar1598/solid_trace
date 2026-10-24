import type { ReactNode } from 'react'
import { cn } from '@/lib/utils'

/**
 * One section of the Overview: a heading and a line about it, set apart by
 * space, not a box. `id` names the section for links and tests.
 */
export function OverviewSection({ id, title, description, children, className }: {
  id: string
  title: string
  description?: ReactNode
  children: ReactNode
  className?: string
}) {
  return (
    <section aria-labelledby={`${id}-title`} data-section={id} className={cn('space-y-4', className)}>
      <div className="space-y-1">
        <h2 id={`${id}-title`} className="text-base font-semibold tracking-tight">{title}</h2>
        {description && <p className="text-sm text-muted-foreground">{description}</p>}
      </div>
      {children}
    </section>
  )
}
