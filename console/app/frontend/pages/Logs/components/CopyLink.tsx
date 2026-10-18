import { Link } from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import type { LinkKind } from '@/lib/logs/explorerState'

/**
 * Copy link in the explorer bar: as shown, with the run's range made
 * absolute, or live, with a relative range kept relative.
 */
export function CopyLink({ onCopy }: { onCopy: (kind: LinkKind) => void }) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger render={<Button variant="outline" />}>
        <Link className="h-3.5 w-3.5" /> Copy link
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="min-w-56">
        <DropdownMenuItem onClick={() => onCopy('shown')}>
          <span className="flex flex-col">
            <span>As shown</span>
            <span className="text-[11px] text-muted-foreground">The same Logs, with the run's times fixed</span>
          </span>
        </DropdownMenuItem>
        <DropdownMenuItem onClick={() => onCopy('live')}>
          <span className="flex flex-col">
            <span>Live</span>
            <span className="text-[11px] text-muted-foreground">The range relative to when it is opened</span>
          </span>
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
