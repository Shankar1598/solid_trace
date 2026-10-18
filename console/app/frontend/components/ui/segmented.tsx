import type { ComponentType, ReactNode } from 'react'
import { cn } from '@/lib/utils'

export interface SegmentedOption<T extends string> {
  value: T
  label: ReactNode
  icon?: ComponentType<{ className?: string }>
}

/**
 * A row of pressed buttons with one on: the Mode, Chart, Viz and Kind
 * switches. Each is a tab stop that Enter or Space presses. `default` is as
 * tall as an input, for toolbars and forms; `sm` sits in a chart's header.
 */
export function Segmented<T extends string>({ label, value, options, onChange, size = 'default', disabled, className }: {
  label: string
  value: T
  options: SegmentedOption<T>[]
  onChange: (value: T) => void
  size?: 'default' | 'sm'
  disabled?: boolean
  className?: string
}) {
  return (
    <div
      role="group"
      aria-label={label}
      className={cn('flex w-fit shrink-0 border', size === 'sm' ? 'h-6 text-[11px]' : 'h-8 text-xs', className)}
    >
      {options.map(({ value: v, label: text, icon: Icon }) => {
        const on = v === value
        return (
          <button
            key={v}
            type="button"
            aria-pressed={on}
            disabled={disabled}
            onClick={() => !on && onChange(v)}
            className={cn(
              'flex items-center gap-1 border-l first:border-l-0 focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-ring disabled:opacity-50',
              size === 'sm' ? 'px-2' : 'px-2.5',
              on ? 'bg-primary font-medium text-primary-foreground' : 'text-muted-foreground hover:bg-muted hover:text-foreground',
            )}
          >
            {Icon && <Icon className="h-3 w-3" />}
            {text}
          </button>
        )
      })}
    </div>
  )
}
