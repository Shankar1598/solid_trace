import { useRef, useState } from 'react'
import { Popover } from '@base-ui/react/popover'
import { format } from 'date-fns'
import { ChevronDown, Clock } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import {
  QUICK_RANGES,
  absoluteFromInputs,
  localInput,
  longerThan,
  parseRange,
  rangeLabel,
  resolveRange,
  sameRange,
  type Parsed,
  type TimeRange,
} from '@/lib/timeRange'
import { cn } from '@/lib/utils'

interface TimeRangePickerProps {
  range: TimeRange
  onChange: (range: TimeRange) => void
  /** The Retention in days: longer ranges are turned away, since their Logs are gone. */
  retentionDays?: number
  className?: string
}

/**
 * The time range of the Logs explorer, Dashboards and Monitors: a button
 * naming the range, opening quick ranges (one click picks one), a box for
 * typed ranges such as `2h` or `yesterday`, and start and end dates in local
 * time.
 */
export function TimeRangePicker({ range, onChange, retentionDays, className }: TimeRangePickerProps) {
  const [open, setOpen] = useState(false)
  const label = rangeLabel(range, new Date())
  const pick = (next: TimeRange) => {
    setOpen(false)
    onChange(next)
  }
  return (
    <Popover.Root open={open} onOpenChange={setOpen}>
      <Popover.Trigger
        aria-label={`Time range: ${label}`}
        className={cn(
          'flex h-8 max-w-full min-w-0 cursor-pointer items-center gap-1.5 border border-input bg-transparent pr-2 pl-2.5 text-xs whitespace-nowrap outline-none hover:bg-muted focus-visible:border-ring focus-visible:ring-1 focus-visible:ring-ring/50 data-[popup-open]:border-ring data-[popup-open]:bg-muted dark:bg-input/30 dark:hover:bg-input/50',
          className,
        )}
      >
        <Clock className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        <span className="truncate">{label}</span>
        <ChevronDown className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Positioner className="isolate z-50" side="bottom" align="start" sideOffset={4} collisionPadding={8}>
          <Popover.Popup
            initialFocus={typedBox}
            className="flex max-h-(--available-height) w-[min(20rem,calc(100vw-1rem))] flex-col overflow-y-auto bg-popover text-xs text-popover-foreground shadow-md ring-1 ring-foreground/10 outline-none"
          >
            {/* Mounted per opening, so each starts from the range as it is. */}
            {open && <PickerForm range={range} retentionDays={retentionDays} onPick={pick} onCancel={() => setOpen(false)} />}
          </Popover.Popup>
        </Popover.Positioner>
      </Popover.Portal>
    </Popover.Root>
  )
}

// The typed box takes focus on opening, so a range can be typed straight
// away; not on touch, where it would open the keyboard over the quick ranges.
// false leaves focus where it is (true would focus the box as the first
// tabbable element).
const typedBox = (openType: string) => openType !== 'touch' && document.getElementById(TYPED_ID)
const TYPED_ID = 'time-range-typed'

function PickerForm({ range, retentionDays, onPick, onCancel }: {
  range: TimeRange
  retentionDays?: number
  onPick: (range: TimeRange) => void
  onCancel: () => void
}) {
  const now = useRef(new Date()).current
  const [typed, setTyped] = useState('')
  // Dates the member has edited; until then they show the range typed, or the current one.
  const [dates, setDates] = useState<{ start: string; end: string } | null>(null)

  const parsed = parseRange(typed, now)
  const typedRange = parsed && 'range' in parsed ? parsed.range : null
  const shown = resolveRange(typedRange ?? range, now)
  const start = dates?.start ?? localInput(shown.from)
  const end = dates?.end ?? localInput(shown.to)

  // What Apply would pick: the typed range, else the edited dates.
  const choice: Parsed | null = parsed ?? (dates ? absoluteFromInputs(dates.start, dates.end) : null)
  const tooLong = choice && 'range' in choice && longerThan(choice.range, retentionDays)
  const problem = choice && 'error' in choice ? choice.error : tooLong ? retentionNote(retentionDays!) : null
  const ready = choice !== null && 'range' in choice && !tooLong

  const apply = () => {
    if (ready) onPick(choice.range)
  }
  const editDate = (part: 'start' | 'end', value: string) => {
    setTyped('')
    setDates({ start, end, [part]: value })
  }

  return (
    <form className="flex flex-col" onSubmit={e => { e.preventDefault(); apply() }}>
      <div className="flex flex-col gap-1.5 p-3 pb-2">
        <label htmlFor={TYPED_ID} className="text-muted-foreground">Type a range</label>
        <input
          id={TYPED_ID}
          value={typed}
          onChange={e => setTyped(e.target.value)}
          placeholder="2h, 3d, last 3 hours, yesterday"
          autoComplete="off"
          spellCheck={false}
          aria-invalid={parsed !== null && problem !== null}
          aria-describedby="time-range-reading"
          className={cn(INPUT, parsed && 'error' in parsed && 'border-destructive')}
        />
        <p id="time-range-reading" aria-live="polite" className={cn('min-h-4 leading-4', problem ? 'text-destructive' : 'text-muted-foreground')}>
          {problem ?? (typedRange ? reading(typedRange, now) : null)}
        </p>
      </div>

      <div className="px-3 pb-3">
        <div className="mb-1.5 text-muted-foreground">Quick range</div>
        <div className="grid grid-cols-3 gap-1.5">
          {QUICK_RANGES.map(q => {
            const quick: TimeRange = { kind: 'relative', span: q.span }
            const current = sameRange(quick, range)
            if (longerThan(quick, retentionDays)) {
              return (
                <Tooltip key={q.span}>
                  <TooltipTrigger
                    type="button"
                    aria-disabled="true"
                    aria-label={`${q.label}: ${retentionNote(retentionDays!)}`}
                    className={cn(QUICK, 'cursor-not-allowed text-muted-foreground/60')}
                  >
                    {q.label}
                  </TooltipTrigger>
                  <TooltipContent>{retentionNote(retentionDays!)}</TooltipContent>
                </Tooltip>
              )
            }
            return (
              <button
                key={q.span}
                type="button"
                aria-pressed={current}
                onClick={() => onPick(quick)}
                className={cn(QUICK, current ? 'border-ring bg-accent text-accent-foreground' : 'hover:bg-muted')}
              >
                {q.label}
              </button>
            )
          })}
        </div>
      </div>

      <div className="flex flex-col gap-2 border-t p-3">
        <DateInput label="Start" value={start} onChange={v => editDate('start', v)} />
        <DateInput label="End" value={end} onChange={v => editDate('end', v)} />
        <p className="leading-4 text-muted-foreground">Dates are in your time zone.</p>
      </div>

      <div className="flex items-center justify-between gap-2 border-t px-3 py-2">
        <Button type="button" variant="outline" size="sm" onClick={onCancel}>Cancel</Button>
        <Button type="submit" size="sm" disabled={!ready}>Apply</Button>
      </div>
    </form>
  )
}

function DateInput({ label, value, onChange }: { label: string; value: string; onChange: (value: string) => void }) {
  return (
    <label className="flex flex-col gap-1">
      <span className="text-muted-foreground">{label}</span>
      <input
        type="datetime-local"
        value={value}
        onChange={e => onChange(e.target.value)}
        className={cn(INPUT, 'scheme-light dark:scheme-dark')}
      />
    </label>
  )
}

const INPUT = 'h-8 w-full border bg-background px-2 text-xs outline-none placeholder:text-muted-foreground focus:border-ring'
const QUICK = 'h-8 border px-1 text-xs outline-none focus-visible:border-ring focus-visible:ring-1 focus-visible:ring-ring/50'

/** What a typed range reads as: its name, and the times it covers now. */
function reading(range: TimeRange, now: Date): string {
  if (range.kind === 'absolute') return rangeLabel(range, now)
  const { from } = resolveRange(range, now)
  const since = format(from, from.getFullYear() === now.getFullYear() ? 'MMM d HH:mm' : 'MMM d, yyyy HH:mm')
  return `${rangeLabel(range, now)}: ${since} until now`
}

function retentionNote(days: number): string {
  return `Longer than this Project's Retention: it keeps Logs for ${days} day${days === 1 ? '' : 's'}.`
}
