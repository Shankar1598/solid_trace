import { Search, X } from 'lucide-react'
import type { Builder, Chip } from '@/lib/logs/builder'
import { cn } from '@/lib/utils'
import { valueText } from './TagMenu'

/**
 * What the Builder shows: its chips once validate has read them. A Query the
 * Builder can't show opens in Query mode instead.
 */
export type BuilderView =
  | { status: 'reading' }
  | { status: 'ready'; builder: Builder }

interface BuilderBarProps {
  view: BuilderView
  onFlip: (index: number) => void
  onRemove: (index: number) => void
  onSearch: (text: string) => void
  onRun: () => void
}

/** Filter chips and the search box: the Builder's controls in the bar. */
export function BuilderBar({ view, onFlip, onRemove, onSearch, onRun }: BuilderBarProps) {
  if (view.status === 'reading') return <span className="flex-1 text-xs text-muted-foreground">Reading the filters…</span>

  const { builder } = view
  return (
    <>
      {builder.filters.map((chip, i) => (
        <ChipView key={`${i}:${chip.field}`} chip={chip} onFlip={() => onFlip(i)} onRemove={() => onRemove(i)} />
      ))}
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

function ChipView({ chip, onFlip, onRemove }: { chip: Chip; onFlip: () => void; onRemove: () => void }) {
  const hides = chip.op === '!='
  return (
    <span className="inline-flex max-w-80 items-center overflow-hidden rounded-full border bg-muted/50 font-mono text-xs">
      <span className="truncate py-1 pl-2.5 pr-1">{chip.field}</span>
      <button
        className={cn('px-1 py-1 font-bold', hides ? 'text-red-600' : 'text-emerald-600')}
        onClick={onFlip}
        title={hides ? 'Show only this instead' : 'Hide this instead'}
      >
        {hides ? '≠' : '='}
      </button>
      <span className="truncate py-1 pr-1" title={`a ${typeof chip.value}`}>
        {valueText(chip.value)}
      </span>
      <button className="px-1.5 py-1 hover:bg-muted" onClick={onRemove} aria-label={`Remove ${chip.field}`}>
        <X className="h-3 w-3" />
      </button>
    </span>
  )
}
