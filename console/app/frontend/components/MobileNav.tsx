import { useEffect, useState } from 'react'
import { usePage } from '@inertiajs/react'
import { Dialog as DialogPrimitive } from '@base-ui/react/dialog'
import { Menu } from 'lucide-react'
import { Dialog, DialogOverlay, DialogPortal, DialogTitle } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { SharedProps } from '@/types'
import { Sidebar } from './Sidebar'

/**
 * The phone's frame, below md where the sidebar is hidden: a bar with the
 * Organization's name and a menu button that slides the sidebar in from the
 * left. Following a link in it closes it.
 */
export function MobileNav() {
  const { url, props } = usePage<SharedProps>()
  const [open, setOpen] = useState(false)
  useEffect(() => setOpen(false), [url])

  if (!props.current_org) return null

  return (
    <div className="md:hidden sticky top-0 z-30 flex h-12 shrink-0 items-center gap-2 border-b bg-card px-2">
      <Dialog open={open} onOpenChange={setOpen}>
        <Button variant="ghost" size="icon" className="h-8 w-8" aria-label="Open menu" onClick={() => setOpen(true)}>
          <Menu className="h-4 w-4" />
        </Button>
        <DialogPortal>
          <DialogOverlay />
          <DialogPrimitive.Popup
            className="data-open:animate-in data-closed:animate-out data-open:slide-in-from-left data-closed:slide-out-to-left fixed inset-y-0 left-0 z-50 flex w-[260px] max-w-[85vw] outline-none duration-150"
          >
            <DialogTitle className="sr-only">Menu</DialogTitle>
            <Sidebar className="flex w-full min-h-full overflow-y-auto" />
          </DialogPrimitive.Popup>
        </DialogPortal>
      </Dialog>
      <div className="h-6 w-6 shrink-0 bg-foreground flex items-center justify-center text-background font-mono font-semibold text-xs">
        {props.current_org.slug.charAt(0).toUpperCase()}
      </div>
      <span className="min-w-0 truncate text-sm font-semibold tracking-tight">{props.current_org.name}</span>
    </div>
  )
}
