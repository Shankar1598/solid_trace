import { cn } from '@/lib/utils'

/** A grey block that pulses where content is loading. Hidden from screen readers: the status around it says what loads. */
function Skeleton({ className, ...props }: React.ComponentProps<'div'>) {
  return <div aria-hidden data-slot="skeleton" className={cn('animate-pulse bg-muted', className)} {...props} />
}

/** A chart's placeholder while its Query runs: a title line over the plot. */
function ChartSkeleton({ label }: { label: string }) {
  return (
    <div role="status" aria-label={label} className="flex h-full min-h-24 flex-col gap-2 p-3">
      <Skeleton className="h-3 w-1/3" />
      <Skeleton className="flex-1" />
    </div>
  )
}

export { Skeleton, ChartSkeleton }
