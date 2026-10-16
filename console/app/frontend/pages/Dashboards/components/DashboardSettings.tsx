import { useState } from 'react'
import { router, useForm } from '@inertiajs/react'
import {
  AlertDialog, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import type { Dashboard } from '@/lib/dashboards/dashboard'
import { QUICK_RANGES } from '@/lib/timeRange'

/** A Dashboard's name, description and default range, and deleting it. */
export function DashboardSettings({ dashboard, orgSlug, search, onClose }: {
  dashboard: Dashboard; orgSlug: string; search: string; onClose: () => void
}) {
  const form = useForm({ name: dashboard.name, description: dashboard.description ?? '', default_range: dashboard.default_range })
  const [deleting, setDeleting] = useState(false)
  const basePath = `/${orgSlug}/dashboards/${dashboard.id}`
  const ranges = QUICK_RANGES.some(r => r.span === dashboard.default_range)
    ? QUICK_RANGES
    : [{ span: dashboard.default_range, label: dashboard.default_range }, ...QUICK_RANGES]
  const items = ranges.map(r => ({ value: r.span, label: r.label }))

  const submit = (e: React.FormEvent) => {
    e.preventDefault()
    form.transform(data => ({ dashboard: data }))
    form.patch(`${basePath}${search}`, { preserveScroll: true, onSuccess: onClose })
  }

  return (
    <>
      <Dialog open onOpenChange={open => !open && onClose()}>
        <DialogContent>
          <form onSubmit={submit} className="space-y-4">
            <DialogHeader>
              <DialogTitle>Dashboard settings</DialogTitle>
            </DialogHeader>
            <div className="space-y-2">
              <Label htmlFor="dashboard-name">Name</Label>
              <Input id="dashboard-name" value={form.data.name} onChange={e => form.setData('name', e.target.value)} />
              {form.errors.name && <p className="text-sm text-destructive">Name {form.errors.name}</p>}
            </div>
            <div className="space-y-2">
              <Label htmlFor="dashboard-description">Description</Label>
              <Textarea id="dashboard-description" rows={2} value={form.data.description} onChange={e => form.setData('description', e.target.value)} />
            </div>
            <div className="space-y-2">
              <Label>Default range</Label>
              <Select value={form.data.default_range} onValueChange={value => value && form.setData('default_range', value)} items={items}>
                <SelectTrigger aria-label="Default range"><SelectValue /></SelectTrigger>
                <SelectContent>
                  {items.map(item => <SelectItem key={item.value} value={item.value}>{item.label}</SelectItem>)}
                </SelectContent>
              </Select>
              <p className="text-xs text-muted-foreground">The range the Dashboard opens with, until another is picked.</p>
            </div>
            <DialogFooter className="sm:justify-between">
              <Button type="button" variant="destructive" onClick={() => setDeleting(true)}>Delete dashboard</Button>
              <div className="flex gap-2">
                <Button type="button" variant="outline" onClick={onClose}>Cancel</Button>
                <Button type="submit" disabled={form.processing}>Save</Button>
              </div>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
      <AlertDialog open={deleting} onOpenChange={setDeleting}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete "{dashboard.name}"?</AlertDialogTitle>
            <AlertDialogDescription>The Dashboard and its charts are deleted for everyone in the Organization.</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <Button variant="destructive" onClick={() => router.delete(basePath)}>Delete dashboard</Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}
