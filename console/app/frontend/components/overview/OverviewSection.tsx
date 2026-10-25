import type { ReactNode } from 'react'
import { Info } from 'lucide-react'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { cn } from '@/lib/utils'

/**
 * One panel of the Overview, framed as a Dashboard's Chart is: a title strip
 * over its body. The line about it sits behind an info mark, so the panels
 * stay short enough for the page to read as one view. `id` names the
 * section for links and tests; `bodyClassName` replaces the body's padding,
 * as a table flush to the frame wants. The panel is `relative` so its
 * screen-reader-only line is placed inside it: placed against the window
 * instead, it would stretch the window past main, which alone scrolls.
 */
export function OverviewSection({ id, title, description, children, className, bodyClassName }: {
  id: string
  title: string
  description?: string
  children: ReactNode
  className?: string
  bodyClassName?: string
}) {
  return (
    <section
      aria-labelledby={`${id}-title`}
      aria-describedby={description ? `${id}-description` : undefined}
      data-section={id}
      className={cn('relative flex min-w-0 flex-col border bg-card', className)}
    >
      <div className="flex shrink-0 items-center gap-1.5 border-b px-3 py-1.5">
        <h2 id={`${id}-title`} className="text-[13px] font-medium">{title}</h2>
        {description && (
          <>
            <p id={`${id}-description`} className="sr-only">{description}</p>
            <Tooltip>
              <TooltipTrigger
                type="button"
                aria-label={`About ${title}`}
                className="text-muted-foreground hover:text-foreground focus-visible:text-foreground"
              >
                <Info className="h-3.5 w-3.5" aria-hidden />
              </TooltipTrigger>
              <TooltipContent className="max-w-sm">{description}</TooltipContent>
            </Tooltip>
          </>
        )}
      </div>
      <div className={cn('min-h-0 flex-1', bodyClassName ?? 'p-3')}>{children}</div>
    </section>
  )
}
