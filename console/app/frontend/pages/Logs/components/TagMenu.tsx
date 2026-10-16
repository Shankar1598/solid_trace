import { createContext, useContext, useState } from 'react'
import { Menu as MenuPrimitive } from '@base-ui/react/menu'
import { ChartLine, Columns3, Copy, Filter, FilterX, Group } from 'lucide-react'
import { copyText } from '@/components/copyText'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { canFilter, type ChipValue } from '@/lib/logs/builder'
import type { AttributeValue } from '@/lib/logs/stream'
import { cn } from '@/lib/utils'

/** The value a tag holds, and the Field it belongs to. */
interface TagPayload {
  field: string
  value: AttributeValue
  /** False where a filter on the value makes no sense, such as an aggregate's cell. */
  filterable: boolean
  /** True where the Field can be a column of the stream: a Log's own Fields. */
  columnable: boolean
}

export type FilterKind = 'show' | 'hide'
/** Show only this / Hide this, or null where filters can't be added. */
export type OnFilter = ((kind: FilterKind, field: string, value: ChipValue) => void) | null
export type SummarizeKind = 'group' | 'chart'
/**
 * Group by and Chart this: a Summarize on the Field (numeric when the value is
 * a number), or null where none can be added.
 */
export type OnSummarize = ((kind: SummarizeKind, field: string, numeric: boolean) => void) | null
/** The stream's columns, and a way to add or remove one. */
export interface ColumnControl {
  shown: string[]
  toggle: (field: string) => void
}

type Handle = MenuPrimitive.Handle<TagPayload>
const TagMenuHandle = createContext<Handle | null>(null)

/**
 * One menu for every tag on the page: tags are detached triggers of it, so a
 * stream of thousands of lines mounts no menu per value.
 */
export function TagMenuProvider({ onFilter, onSummarize, columns, children }: {
  onFilter: OnFilter; onSummarize: OnSummarize; columns: ColumnControl; children: React.ReactNode
}) {
  const [handle] = useState(() => MenuPrimitive.createHandle<TagPayload>())

  return (
    <TagMenuHandle.Provider value={handle}>
      {children}
      <DropdownMenu handle={handle}>
        {({ payload }) => (payload ? <TagMenuContent tag={payload as TagPayload} onFilter={onFilter} onSummarize={onSummarize} columns={columns} /> : null)}
      </DropdownMenu>
    </TagMenuHandle.Provider>
  )
}

function TagMenuContent({ tag, onFilter, onSummarize, columns }: {
  tag: TagPayload; onFilter: OnFilter; onSummarize: OnSummarize; columns: ColumnControl
}) {
  const { field, value, filterable, columnable } = tag
  const column = columnable && columns.shown.includes(field)
  // time can't be filtered on yet: the time range does that; nor grouped by, but in bins.
  const filter = onFilter && filterable && field !== 'time' && canFilter(value) ? onFilter : null
  const summarize = onSummarize && filterable && field !== 'time' ? onSummarize : null
  const numeric = typeof value === 'number'

  return (
    <DropdownMenuContent className="min-w-48 max-w-96">
      {/* base-ui's GroupLabel throws outside a Group. */}
      <DropdownMenuGroup>
        <DropdownMenuLabel className="font-mono truncate">{field} = {valueText(value, 80)}</DropdownMenuLabel>
      </DropdownMenuGroup>
      <DropdownMenuSeparator />
      <DropdownMenuItem disabled={!filter} onClick={() => filter?.('show', field, value as ChipValue)}>
        <Filter className="h-3.5 w-3.5" /> Show only this
      </DropdownMenuItem>
      <DropdownMenuItem disabled={!filter} onClick={() => filter?.('hide', field, value as ChipValue)}>
        <FilterX className="h-3.5 w-3.5" /> Hide this
      </DropdownMenuItem>
      <DropdownMenuItem disabled={!summarize} onClick={() => summarize?.('group', field, numeric)}>
        <Group className="h-3.5 w-3.5" /> Group by
      </DropdownMenuItem>
      <DropdownMenuItem disabled={!summarize} onClick={() => summarize?.('chart', field, numeric)}>
        <ChartLine className="h-3.5 w-3.5" /> Chart this
      </DropdownMenuItem>
      <DropdownMenuItem onClick={() => copy(value)}>
        <Copy className="h-3.5 w-3.5" /> Copy value
      </DropdownMenuItem>
      {columnable && (
        <DropdownMenuItem disabled={column && columns.shown.length === 1} onClick={() => columns.toggle(field)}>
          <Columns3 className="h-3.5 w-3.5" /> {column ? 'Remove column' : 'Add as column'}
        </DropdownMenuItem>
      )}
    </DropdownMenuContent>
  )
}

/**
 * A value shown anywhere in the explorer; clicking it opens the tag menu.
 * label names a trigger that shows no value, such as an icon.
 */
export function Tag({ field, value, filterable = true, columnable = false, label, className, children }: {
  field: string; value: AttributeValue; filterable?: boolean; columnable?: boolean; label?: string; className?: string; children: React.ReactNode
}) {
  const handle = useContext(TagMenuHandle)
  return (
    <DropdownMenuTrigger
      handle={handle ?? undefined}
      payload={{ field, value, filterable, columnable }}
      onClick={e => e.stopPropagation()}
      aria-label={label}
      title={label}
      className={cn('rounded-sm px-0.5 text-left cursor-pointer hover:bg-primary/15 hover:outline hover:outline-1 hover:outline-primary/40 data-popup-open:bg-primary/15', className)}
    >
      {children}
    </DropdownMenuTrigger>
  )
}

/** A value as text: strings as they are, anything else as JSON. */
export function valueText(value: AttributeValue, max = Infinity): string {
  const text = typeof value === 'string' ? value : JSON.stringify(value)
  return text.length > max ? `${text.slice(0, max)}…` : text
}

/** Copies a value as text, saying whether it worked: "<what> copied". */
export function copy(value: AttributeValue, what = 'Value') {
  copyText(valueText(value), what)
}
