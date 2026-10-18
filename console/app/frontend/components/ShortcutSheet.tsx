import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { SHORTCUTS, keyLabel } from '@/lib/shortcuts'
import { onMac } from './useShortcut'

/** The ? sheet: every shortcut in the registry, with its keys. */
export function ShortcutSheet({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  const mac = onMac()
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Keyboard shortcuts</DialogTitle>
          <DialogDescription>Typing in a box, only {keyLabel(['Mod', 'k'], mac)}, {keyLabel(['Mod', 'Enter'], mac)} and Esc work.</DialogDescription>
        </DialogHeader>
        <dl className="grid grid-cols-[auto_1fr] items-center gap-x-4 gap-y-2" aria-label="Shortcuts">
          {SHORTCUTS.map(s => (
            <div key={s.id} className="contents">
              <dt><kbd className="border bg-muted px-1.5 py-0.5 font-mono text-[11px]">{keyLabel(s.keys, mac)}</kbd></dt>
              <dd>{s.does}</dd>
            </div>
          ))}
        </dl>
      </DialogContent>
    </Dialog>
  )
}
