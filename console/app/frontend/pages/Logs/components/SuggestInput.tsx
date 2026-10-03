import { useState } from 'react'
import { cn } from '@/lib/utils'

export interface Suggestion {
  /** What choosing it puts in the input. */
  value: string
  /** Shown on the right, such as a Field's type. */
  hint?: string
}

const SHOWN = 50

/**
 * A text input that suggests as it's focused and typed in: the suggestions
 * containing the text, chosen with a click, or the arrows and Enter. Any
 * text can still be typed, for Fields not in the Logs read.
 */
export function SuggestInput({ value, onChange, suggestions, placeholder, autoFocus, className }: {
  value: string
  onChange: (value: string) => void
  suggestions: Suggestion[]
  placeholder?: string
  autoFocus?: boolean
  className?: string
}) {
  const [open, setOpen] = useState(false)
  const [active, setActive] = useState(-1)
  const typed = value.trim().toLowerCase()
  const shown = suggestions
    .filter(s => s.value.toLowerCase().includes(typed) && s.value !== value)
    .sort((a, b) => Number(!a.value.toLowerCase().startsWith(typed)) - Number(!b.value.toLowerCase().startsWith(typed)))
    .slice(0, SHOWN)
  const listOpen = open && shown.length > 0

  const choose = (s: Suggestion) => {
    onChange(s.value)
    setActive(-1)
    setOpen(false)
  }

  return (
    <div className="relative">
      <input
        autoFocus={autoFocus}
        value={value}
        placeholder={placeholder}
        className={className}
        role="combobox"
        aria-expanded={listOpen}
        aria-autocomplete="list"
        onFocus={() => setOpen(true)}
        onBlur={() => setOpen(false)}
        onChange={e => {
          onChange(e.target.value)
          setOpen(true)
          setActive(-1)
        }}
        onKeyDown={e => {
          if (!listOpen) {
            if (e.key === 'ArrowDown') setOpen(true)
            return
          }
          if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
            e.preventDefault()
            const step = e.key === 'ArrowDown' ? 1 : -1
            setActive(a => (a + step + shown.length) % shown.length)
          } else if (e.key === 'Enter' && active >= 0) {
            // Choose rather than submit the form.
            e.preventDefault()
            choose(shown[active])
          } else if (e.key === 'Escape') {
            // Close the list, not the popover it's in.
            e.stopPropagation()
            setOpen(false)
          }
        }}
      />
      {listOpen && (
        <ul role="listbox" className="absolute inset-x-0 top-full z-10 mt-1 max-h-56 overflow-y-auto border bg-popover py-1 shadow-md">
          {shown.map((s, i) => (
            <li
              key={s.value}
              role="option"
              aria-selected={i === active}
              // Keep the input's focus, so blur doesn't close the list first.
              onMouseDown={e => { e.preventDefault(); choose(s) }}
              onMouseEnter={() => setActive(i)}
              className={cn('flex cursor-pointer items-center gap-2 px-2 py-1 font-mono', i === active && 'bg-accent text-accent-foreground')}
            >
              <span className="min-w-0 flex-1 truncate">{s.value}</span>
              {s.hint && <span className="shrink-0 font-sans text-[10px] text-muted-foreground">{s.hint}</span>}
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
