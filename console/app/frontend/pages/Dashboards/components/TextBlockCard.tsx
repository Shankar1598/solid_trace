import { Copy, MoreHorizontal, Pencil, Trash2 } from 'lucide-react'
import { Markdown } from '@/components/Markdown'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import type { TextBlock } from '@/lib/dashboards/dashboard'
import { cn } from '@/lib/utils'

/**
 * A Text block on the Dashboard: its Markdown, under its title when it has
 * one, with a menu to edit, duplicate or delete it. It runs nothing, so the
 * range, the filters and a refresh don't change it. In Edit mode the header
 * is the handle that drags the card, so a block with no title shows one
 * then.
 */
export function TextBlockCard({ block, editing, onEdit, onDuplicate, onDelete }: {
  block: TextBlock
  editing: boolean
  onEdit: () => void
  onDuplicate: () => void
  onDelete: () => void
}) {
  const title = block.title?.trim() ?? ''
  const menu = (
    <DropdownMenu>
      <DropdownMenuTrigger render={<Button variant="ghost" size="icon" className="h-6 w-6" aria-label={`Menu for ${title || 'text'}`} />}>
        <MoreHorizontal className="h-3.5 w-3.5" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="min-w-44">
        <DropdownMenuItem onClick={onEdit}><Pencil className="h-3.5 w-3.5" /> Edit</DropdownMenuItem>
        <DropdownMenuItem onClick={onDuplicate}><Copy className="h-3.5 w-3.5" /> Duplicate</DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem onClick={onDelete} className="text-destructive"><Trash2 className="h-3.5 w-3.5" /> Delete</DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )

  return (
    <div className="relative flex h-full flex-col overflow-hidden border bg-card" data-text-block={title}>
      {title || editing ? (
        <div className="flex shrink-0 items-center gap-2 border-b px-3 py-1">
          <h2 className={cn('min-w-0 flex-1 truncate text-[13px] font-medium', editing && 'dashboard-chart-handle cursor-move', !title && 'text-muted-foreground')} title={title}>
            {title || 'Text'}
          </h2>
          {menu}
        </div>
      ) : (
        // With no title, the menu is in the text's top-right corner.
        <div className="absolute top-1 right-1 bg-card">{menu}</div>
      )}
      <div className={cn('min-h-0 flex-1 overflow-auto px-3 py-2', !title && !editing && 'pr-9')} data-text-body>
        <Markdown text={block.text} />
      </div>
    </div>
  )
}
