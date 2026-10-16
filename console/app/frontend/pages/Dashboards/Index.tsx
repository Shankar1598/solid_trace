import { useState } from 'react'
import { Link, useForm, usePage } from '@inertiajs/react'
import { formatDistanceToNow } from 'date-fns'
import { LayoutDashboard, Plus } from 'lucide-react'
import DashboardLayout from '@/components/layouts/DashboardLayout'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import type { Dashboard } from '@/lib/dashboards/dashboard'
import type { SharedProps } from '@/types'

/** The Organization's Dashboards: each one's name, chart count and when it last changed. */
export default function DashboardsIndex({ dashboards }: { dashboards: Dashboard[] }) {
  const orgSlug = usePage<SharedProps>().props.current_org?.slug ?? ''
  const [creating, setCreating] = useState(false)

  return (
    <DashboardLayout reserveBreadcrumbSpace>
      <div className="space-y-6">
        <div className="flex items-center justify-between">
          <h1 className="text-3xl font-bold tracking-tight">Dashboards</h1>
          <Button onClick={() => setCreating(true)}>
            <Plus className="mr-2 h-4 w-4" />
            New dashboard
          </Button>
        </div>

        {dashboards.length === 0 ? (
          <Card className="border-dashed">
            <CardContent className="flex flex-col items-center justify-center py-12 text-center">
              <LayoutDashboard className="mb-4 h-12 w-12 text-muted-foreground opacity-50" />
              <h3 className="text-lg font-medium">No dashboards yet</h3>
              <p className="mb-4 max-w-md text-sm text-muted-foreground">
                Make one here and add charts to it, or chart a Query in the Logs explorer and press Add to dashboard beside the chart switch.
              </p>
              <Button onClick={() => setCreating(true)}>New dashboard</Button>
            </CardContent>
          </Card>
        ) : (
          <div className="overflow-hidden rounded-md border">
            <table className="w-full text-sm">
              <thead className="bg-muted text-left text-xs text-muted-foreground">
                <tr>
                  <th className="px-4 py-2 font-medium">Name</th>
                  <th className="px-4 py-2 font-medium">Charts</th>
                  <th className="px-4 py-2 font-medium">Updated</th>
                </tr>
              </thead>
              <tbody>
                {dashboards.map(d => (
                  <tr key={d.id} className="border-t hover:bg-muted/50">
                    <td className="px-4 py-2">
                      <Link href={`/${orgSlug}/dashboards/${d.id}`} className="font-medium hover:underline">{d.name}</Link>
                      {d.description && <div className="truncate text-xs text-muted-foreground">{d.description}</div>}
                    </td>
                    <td className="px-4 py-2 tabular-nums">{d.chart_count}</td>
                    <td className="px-4 py-2 text-muted-foreground">{formatDistanceToNow(new Date(d.updated_at))} ago</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
      <NewDashboardDialog open={creating} onOpenChange={setCreating} orgSlug={orgSlug} />
    </DashboardLayout>
  )
}

function NewDashboardDialog({ open, onOpenChange, orgSlug }: { open: boolean; onOpenChange: (open: boolean) => void; orgSlug: string }) {
  const form = useForm({ name: '', description: '' })
  const submit = (e: React.FormEvent) => {
    e.preventDefault()
    form.transform(data => ({ dashboard: data }))
    form.post(`/${orgSlug}/dashboards`)
  }
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <form onSubmit={submit} className="space-y-4">
          <DialogHeader>
            <DialogTitle>New dashboard</DialogTitle>
            <DialogDescription>Charts of Queries on any of the Organization's Projects, over one time range.</DialogDescription>
          </DialogHeader>
          <div className="space-y-2">
            <Label htmlFor="dashboard-name">Name</Label>
            <Input id="dashboard-name" value={form.data.name} onChange={e => form.setData('name', e.target.value)} autoFocus />
            {form.errors.name && <p className="text-sm text-destructive">Name {form.errors.name}</p>}
          </div>
          <div className="space-y-2">
            <Label htmlFor="dashboard-description">Description</Label>
            <Textarea id="dashboard-description" value={form.data.description} onChange={e => form.setData('description', e.target.value)} rows={2} />
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>Cancel</Button>
            <Button type="submit" disabled={form.processing}>Create dashboard</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
