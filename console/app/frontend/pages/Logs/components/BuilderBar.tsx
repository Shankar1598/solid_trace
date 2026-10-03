import { Search } from 'lucide-react'
import type { Builder } from '@/lib/logs/builder'

/**
 * What the Builder shows: its chips once validate has read them. A Query the
 * Builder can't show opens in Query mode instead.
 */
export type BuilderView =
  | { status: 'reading' }
  | { status: 'ready'; builder: Builder }

interface BuilderBarProps {
  view: BuilderView
  onSearch: (text: string) => void
  onRun: () => void
}

/** The search box: the Builder's control in the bar. Its other clauses are in the BuilderPanel. */
export function BuilderBar({ view, onSearch, onRun }: BuilderBarProps) {
  if (view.status === 'reading') return <span className="flex-1 text-xs text-muted-foreground">Reading the Query…</span>

  const { builder } = view
  return (
    <>
      <form
        className="flex h-8 min-w-48 flex-1 items-center gap-2 rounded-md border px-2"
        onSubmit={e => { e.preventDefault(); onRun() }}
      >
        <Search className="h-3.5 w-3.5 text-muted-foreground" />
        <input
          value={builder.search}
          onChange={e => onSearch(e.target.value)}
          placeholder="Search messages and Attributes…"
          aria-label="Search Logs"
          className="flex-1 bg-transparent text-xs outline-none"
        />
      </form>
    </>
  )
}
