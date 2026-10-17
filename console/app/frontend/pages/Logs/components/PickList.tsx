import { Fragment, useEffect, useRef, useState } from 'react'
import { Check, Search } from 'lucide-react'
import { narrow, type PickOption } from '@/lib/logs/pick'
import { cn } from '@/lib/utils'

/**
 * The highlighted option and the keys that move it: the arrows, and Enter to
 * pick it. Nothing is highlighted at -1, so Enter goes on to submit a form.
 */
export function useHighlight(options: PickOption[], start: number, onPick: (option: PickOption) => void) {
  const [active, setActive] = useState(start)
  const at = Math.min(active, options.length - 1)
  const onKeyDown = (e: React.KeyboardEvent) => {
    if (options.length === 0) return
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault()
      const step = e.key === 'ArrowDown' ? 1 : -1
      setActive(at < 0 ? (step > 0 ? 0 : options.length - 1) : (at + step + options.length) % options.length)
    } else if (e.key === 'Enter' && at >= 0) {
      e.preventDefault()
      if (!options[at].disabled) onPick(options[at])
    }
  }
  return { active: at, setActive, onKeyDown }
}

/** Options under their group headings, the highlighted one shaded and the chosen ones ticked. */
export function OptionList({ options, active, onActive, onPick, chosen, empty, className }: {
  options: PickOption[]
  active: number
  onActive: (index: number) => void
  onPick: (option: PickOption) => void
  /** Ticked, with room kept for ticks, when given. */
  chosen?: string[]
  /** Shown when there are no options. */
  empty?: string
  className?: string
}) {
  const list = useRef<HTMLUListElement>(null)
  useEffect(() => {
    list.current?.querySelector('[data-active]')?.scrollIntoView({ block: 'nearest' })
  }, [active])

  if (options.length === 0) return empty ? <p className="px-2 py-3 text-center text-muted-foreground">{empty}</p> : null
  return (
    <ul ref={list} role="listbox" className={cn('overflow-y-auto py-1', className)}>
      {options.map((o, i) => (
        <Fragment key={o.value}>
          {o.group && o.group !== options[i - 1]?.group && (
            <li role="presentation" className="caps-label px-2 pb-1 pt-2 first:pt-1">
              {o.group}
            </li>
          )}
          <li
            role="option"
            aria-selected={i === active}
            aria-disabled={o.disabled || undefined}
            data-active={i === active || undefined}
            // Keep the focus where it is, in the input typed in.
            onMouseDown={e => e.preventDefault()}
            onClick={() => !o.disabled && onPick(o)}
            onMouseMove={() => i !== active && onActive(i)}
            className={cn(
              'flex h-7 cursor-pointer items-center gap-2 px-2',
              i === active && 'bg-accent text-accent-foreground',
              o.disabled && 'cursor-default text-muted-foreground',
            )}
          >
            {chosen && (
              <span className="w-3 shrink-0">{chosen.includes(o.value) && <Check className="h-3 w-3" />}</span>
            )}
            <span className="min-w-0 flex-1 truncate font-mono">{o.label ?? o.value}</span>
            {o.hint && <span className="shrink-0 text-[10px] text-muted-foreground">{o.hint}</span>}
          </li>
        </Fragment>
      ))}
    </ul>
  )
}

/**
 * A box to type in over a list of options, narrowing it. With typed set, what's
 * typed is offered too when no option is just that, such as a Field not in the
 * Logs read.
 */
export function PickList({ options, onPick, placeholder, typed, chosen, empty }: {
  options: PickOption[]
  onPick: (value: string) => void
  placeholder: string
  typed?: { hint: string }
  chosen?: string[]
  empty: string
}) {
  const [query, setQuery] = useState('')
  const text = query.trim()
  const narrowed = narrow(options, query)
  const shown = typed && text !== '' && !options.some(o => o.value === text)
    ? [...narrowed, { value: text, label: `Use "${text}"`, hint: typed.hint }]
    : narrowed
  const { active, setActive, onKeyDown } = useHighlight(shown, 0, o => onPick(o.value))

  return (
    <div className="flex min-h-0 flex-col">
      <div className="flex h-8 shrink-0 items-center gap-2 border-b px-2">
        <Search className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        <input
          autoFocus
          value={query}
          placeholder={placeholder}
          aria-label={placeholder}
          onChange={e => { setQuery(e.target.value); setActive(0) }}
          onKeyDown={onKeyDown}
          className="h-full min-w-0 flex-1 bg-transparent outline-none placeholder:text-muted-foreground"
        />
      </div>
      {/* Tall enough that a list longer than it shows part of a row at its foot, so it reads as one to scroll. */}
      <OptionList options={shown} active={active} onActive={setActive} onPick={o => onPick(o.value)} chosen={chosen} empty={empty} className="max-h-72" />
    </div>
  )
}

/** A short list to pick from with the arrows and Enter, or a click, without typing. */
export function MenuList({ options, onPick, chosen, label }: {
  options: PickOption[]
  onPick: (value: string) => void
  chosen?: string[]
  label: string
}) {
  const { active, setActive, onKeyDown } = useHighlight(options, Math.max(0, options.findIndex(o => !o.disabled)), o => onPick(o.value))
  // React's autoFocus is for form controls only.
  const box = useRef<HTMLDivElement>(null)
  useEffect(() => box.current?.focus(), [])
  return (
    <div ref={box} tabIndex={-1} aria-label={label} onKeyDown={onKeyDown} className="outline-none">
      <OptionList options={options} active={active} onActive={setActive} onPick={o => onPick(o.value)} chosen={chosen} />
    </div>
  )
}
