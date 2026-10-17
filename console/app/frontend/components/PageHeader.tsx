import type { ReactNode } from 'react'
import Breadcrumbs, { type BreadcrumbItem } from './Breadcrumbs'
import { cn } from '@/lib/utils'

interface PageHeaderProps {
  title: ReactNode
  /** Beside the title: its status, as a Badge. */
  badge?: ReactNode
  /** One line under the title. */
  description?: ReactNode
  /** Above the title on a nested page. */
  breadcrumbs?: BreadcrumbItem[]
  /** On the right: the page's buttons. */
  actions?: ReactNode
  className?: string
}

/**
 * Every page's header: breadcrumbs, title and description on the left, the
 * page's buttons on the right. A top-level page keeps the breadcrumbs' line
 * empty, so a title sits at the same height on every page.
 */
export function PageHeader({ title, badge, description, breadcrumbs, actions, className }: PageHeaderProps) {
  return (
    <header className={cn('mb-6 space-y-1.5', className)}>
      <div className="min-h-5">{breadcrumbs && <Breadcrumbs items={breadcrumbs} />}</div>
      <div className="flex flex-wrap items-start justify-between gap-x-6 gap-y-3">
        <div className="min-w-0 space-y-1">
          <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
            <h1 className="min-w-0 break-words text-xl font-semibold tracking-tight">{title}</h1>
            {badge}
          </div>
          {description && <div className="text-sm text-muted-foreground">{description}</div>}
        </div>
        {actions && <div className="flex flex-wrap items-center gap-2">{actions}</div>}
      </div>
    </header>
  )
}
