import type { Builder } from '@/lib/logs/builder'

/**
 * What the Builder shows: its chips once validate has read them. A Query the
 * Builder can't show opens in Query mode instead.
 */
export type BuilderView =
  | { status: 'reading' }
  | { status: 'ready'; builder: Builder }

/** The Builder's place in the bar: its clauses, the search among them, are in the BuilderPanel. */
export function BuilderBar({ view }: { view: BuilderView }) {
  return (
    // On a phone the hint would wrap to three lines and push Run onto a row of its own.
    <span className="flex-1 text-[11px] text-muted-foreground">
      {view.status === 'reading' ? 'Reading the Query…' : <span className="hidden sm:inline">Each change in the Builder runs at once.</span>}
    </span>
  )
}
