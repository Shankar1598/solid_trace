import { router, useForm } from '@inertiajs/react'
import { ArrowDown, ArrowUp, ChevronRight, MoreHorizontal, Pencil, Plus, Trash2, Type } from 'lucide-react'
import {
  AlertDialog, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import {
  DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import type { DashboardSection } from '@/lib/dashboards/dashboard'
import { cn } from '@/lib/utils'

const chartsText = (n: number) => `${n} ${n === 1 ? 'chart' : 'charts'}`

/**
 * A Section's header row: a chevron that folds or unfolds it for this viewer,
 * its title, the count of its charts while folded, and a menu to add a chart
 * or a Text block in it, rename it, move it up or down, or delete it.
 */
export function SectionHeader({ section, chartCount, folded, first, last, onToggle, onAddChart, onAddText, onRename, onMove, onDelete }: {
  section: DashboardSection
  chartCount: number
  folded: boolean
  first: boolean
  last: boolean
  onToggle: () => void
  onAddChart: () => void
  onAddText: () => void
  onRename: () => void
  /** -1 moves it up one place, 1 down one. */
  onMove: (by: -1 | 1) => void
  onDelete: () => void
}) {
  return (
    <div className="flex items-center gap-1 border-b pb-1" data-section={section.title}>
      <button
        type="button"
        className="flex min-w-0 flex-1 items-center gap-1.5 text-left"
        aria-expanded={!folded}
        aria-label={`${folded ? 'Unfold' : 'Fold'} ${section.title}`}
        onClick={onToggle}
      >
        <ChevronRight className={cn('h-4 w-4 shrink-0 text-muted-foreground transition-transform', !folded && 'rotate-90')} />
        <h2 className="truncate text-sm font-medium" title={section.title}>{section.title}</h2>
        {folded && <span className="shrink-0 text-xs text-muted-foreground">{chartsText(chartCount)}</span>}
      </button>
      <DropdownMenu>
        <DropdownMenuTrigger render={<Button variant="ghost" size="icon" className="h-6 w-6" aria-label={`Menu for section ${section.title}`} />}>
          <MoreHorizontal className="h-3.5 w-3.5" />
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="min-w-44">
          <DropdownMenuItem onClick={onAddChart}><Plus className="h-3.5 w-3.5" /> Add chart here</DropdownMenuItem>
          <DropdownMenuItem onClick={onAddText}><Type className="h-3.5 w-3.5" /> Add text here</DropdownMenuItem>
          <DropdownMenuItem onClick={onRename}><Pencil className="h-3.5 w-3.5" /> Rename</DropdownMenuItem>
          <DropdownMenuItem onClick={() => onMove(-1)} disabled={first}><ArrowUp className="h-3.5 w-3.5" /> Move up</DropdownMenuItem>
          <DropdownMenuItem onClick={() => onMove(1)} disabled={last}><ArrowDown className="h-3.5 w-3.5" /> Move down</DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem onClick={onDelete} className="text-destructive"><Trash2 className="h-3.5 w-3.5" /> Delete</DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  )
}

/** Adds a Section, last, or renames one. Saving posts through Inertia, which reloads the Dashboard. */
export function SectionDialog({ section, orgSlug, dashboardId, search, onClose }: {
  /** The Section renamed, or null for a new one. */
  section: DashboardSection | null
  orgSlug: string
  dashboardId: number
  /** The Dashboard's URL search, so it keeps its range and filter values after saving. */
  search: string
  onClose: () => void
}) {
  const form = useForm({ title: section?.title ?? '' })
  const basePath = `/${orgSlug}/dashboards/${dashboardId}/sections`

  const submit = (e: React.FormEvent) => {
    e.preventDefault()
    form.transform(data => ({ section: data }))
    const visit = { preserveScroll: true, preserveState: true, onSuccess: onClose }
    if (section) form.patch(`${basePath}/${section.id}${search}`, visit)
    else form.post(`${basePath}${search}`, visit)
  }

  return (
    <Dialog open onOpenChange={open => !open && onClose()}>
      <DialogContent>
        <form onSubmit={submit} className="space-y-4">
          <DialogHeader>
            <DialogTitle>{section ? 'Rename section' : 'Add a section'}</DialogTitle>
          </DialogHeader>
          <div className="space-y-2">
            <Label htmlFor="section-title">Title</Label>
            <Input id="section-title" value={form.data.title} maxLength={200} autoFocus onChange={e => form.setData('title', e.target.value)} />
            {/* Title's errors, and the Dashboard's when it has as many Sections as it can. */}
            {Object.entries(form.errors as Record<string, string>).map(([field, message]) => (
              <p key={field} className="text-sm text-destructive">{field[0].toUpperCase()}{field.slice(1)} {message}</p>
            ))}
            {!section && <p className="text-xs text-muted-foreground">The section goes under the others. Add charts to it from its menu, or pick it in a chart's editor.</p>}
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={onClose}>Cancel</Button>
            <Button type="submit" disabled={form.processing}>{section ? 'Save' : 'Add section'}</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

/** Asks before a Section is deleted; its charts stay on the Dashboard in no Section. */
export function DeleteSectionDialog({ section, chartCount, orgSlug, dashboardId, search, onClose }: {
  section: DashboardSection | null
  chartCount: number
  orgSlug: string
  dashboardId: number
  search: string
  onClose: () => void
}) {
  const remove = () => section && router.delete(`/${orgSlug}/dashboards/${dashboardId}/sections/${section.id}${search}`, {
    preserveScroll: true, preserveState: true, onFinish: onClose,
  })

  return (
    <AlertDialog open={section !== null} onOpenChange={open => !open && onClose()}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Delete section "{section?.title}"?</AlertDialogTitle>
          <AlertDialogDescription>
            The section is removed for everyone.{' '}
            {chartCount > 0 ? `Its ${chartsText(chartCount)} stay on the Dashboard, under the charts in no section.` : 'It has no charts.'}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <Button variant="destructive" onClick={remove}>Delete section</Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
