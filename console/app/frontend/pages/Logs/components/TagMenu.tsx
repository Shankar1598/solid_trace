import { createContext, useContext, useState } from 'react'
import { Menu as MenuPrimitive } from '@base-ui/react/menu'
import { Copy, Filter, FilterX } from 'lucide-react'
import { toast } from 'sonner'
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
}

export type FilterKind = 'show' | 'hide'
/** Show only this / Hide this, or null where filters can't be added. */
export type OnFilter = ((kind: FilterKind, field: string, value: ChipValue) => void) | null

type Handle = MenuPrimitive.Handle<TagPayload>
const TagMenuHandle = createContext<Handle | null>(null)

/**
 * One menu for every tag on the page: tags are detached triggers of it, so a
 * stream of thousands of lines mounts no menu per value.
 */
export function TagMenuProvider({ onFilter, children }: { onFilter: OnFilter; children: React.ReactNode }) {
  const [handle] = useState(() => MenuPrimitive.createHandle<TagPayload>())

  return (
    <TagMenuHandle.Provider value={handle}>
      {children}
      <DropdownMenu handle={handle}>
        {({ payload }) => (payload ? <TagMenuContent tag={payload as TagPayload} onFilter={onFilter} /> : null)}
      </DropdownMenu>
    </TagMenuHandle.Provider>
  )
}

function TagMenuContent({ tag, onFilter }: { tag: TagPayload; onFilter: OnFilter }) {
  const { field, value, filterable } = tag
  // time can't be filtered on yet: the time range does that.
  const filter = onFilter && filterable && field !== 'time' && canFilter(value) ? onFilter : null

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
      <DropdownMenuItem onClick={() => copy(value)}>
        <Copy className="h-3.5 w-3.5" /> Copy value
      </DropdownMenuItem>
    </DropdownMenuContent>
  )
}

/** A value shown anywhere in the explorer; clicking it opens the tag menu. */
export function Tag({ field, value, filterable = true, className, children }: {
  field: string; value: AttributeValue; filterable?: boolean; className?: string; children: React.ReactNode
}) {
  const handle = useContext(TagMenuHandle)
  return (
    <DropdownMenuTrigger
      handle={handle ?? undefined}
      payload={{ field, value, filterable }}
      onClick={e => e.stopPropagation()}
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

function copy(value: AttributeValue) {
  navigator.clipboard?.writeText(valueText(value))
    .then(() => toast.success('Copied'))
    .catch(() => toast.error('Could not copy the value'))
}
