import { Link } from '@inertiajs/react'
import { cn } from '@/lib/utils'

/**
 * The tabs of the Agent sessions entry: the Agent sessions, and the Review
 * records that their final replies hold. Each tab is its own page, so the
 * tabs are links, and the open one is the current page.
 */
export function AgentSessionsTabs({ slug, current }: { slug: string; current: 'agent_sessions' | 'review_records' }) {
  const tabs = [
    { id: 'agent_sessions', label: 'Agent sessions', href: `/${slug}/agent_sessions` },
    { id: 'review_records', label: 'Review records', href: `/${slug}/review_records` },
  ] as const
  return (
    <nav aria-label="Agent sessions tabs" className="-mt-2 mb-6 flex gap-5 border-b">
      {tabs.map(tab => {
        const open = tab.id === current
        return (
          <Link
            key={tab.id}
            href={tab.href}
            aria-current={open ? 'page' : undefined}
            className={cn(
              '-mb-px border-b-2 pb-2 text-sm font-medium focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring',
              open ? 'border-foreground text-foreground' : 'border-transparent text-muted-foreground hover:text-foreground',
            )}
          >
            {tab.label}
          </Link>
        )
      })}
    </nav>
  )
}
