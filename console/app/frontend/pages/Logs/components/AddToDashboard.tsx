import { useState } from 'react'
import { Link, usePage } from '@inertiajs/react'
import { LayoutDashboard } from 'lucide-react'
import { toast } from 'sonner'
import { Button, buttonVariants } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { addChart, createDashboard } from '@/lib/dashboards/api'
import { titleOfQuery, type DashboardViz } from '@/lib/dashboards/dashboard'
import type { SharedProps } from '@/types'

const NEW = 'new'

/**
 * Add to dashboard, beside the explorer's chart switch: saves the Project,
 * the Query that ran and the chart shown to a Dashboard, a new one or one
 * of the Organization's, then links to it. The explorer stays as it is.
 */
export function AddToDashboard({ projectId, query, viz }: { projectId: number; query: string; viz: DashboardViz }) {
  const { props } = usePage<SharedProps>()
  const orgSlug = props.current_org?.slug ?? ''
  // Dashboards made here since the page loaded, which the shared props don't have yet.
  const [made, setMade] = useState<{ id: number; name: string }[]>([])
  const dashboards = [...(props.org_dashboards ?? []), ...made.filter(m => !props.org_dashboards?.some(d => d.id === m.id))]
  const [open, setOpen] = useState(false)
  const [target, setTarget] = useState<string>(NEW)
  const [name, setName] = useState('')
  const [title, setTitle] = useState('')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [added, setAdded] = useState<{ id: number; name: string } | null>(null)

  const start = () => {
    setTarget(dashboards.length > 0 ? String(dashboards[0].id) : NEW)
    setName('')
    setTitle(titleOfQuery(query))
    setError(null)
    setAdded(null)
    setOpen(true)
  }

  const add = async (e: React.FormEvent) => {
    e.preventDefault()
    setSaving(true)
    setError(null)
    try {
      let dashboard = dashboards.find(d => String(d.id) === target)
      if (!dashboard) {
        const created = await createDashboard(orgSlug, name.trim())
        dashboard = { id: created.id, name: created.name }
        setMade(m => [...m, dashboard!])
        setTarget(String(created.id))
      }
      await addChart(orgSlug, dashboard.id, { project_id: projectId, title: title.trim() || titleOfQuery(query), query, viz })
      setAdded(dashboard)
      toast.success(`Chart added to ${dashboard.name}`)
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : 'The chart was not added. Try again.')
    } finally {
      setSaving(false)
    }
  }

  const items = [...dashboards.map(d => ({ value: String(d.id), label: d.name })), { value: NEW, label: 'New dashboard…' }]

  return (
    <>
      <button
        type="button"
        onClick={start}
        className="flex h-6 shrink-0 items-center gap-1 border px-2 text-[11px] text-muted-foreground hover:bg-muted hover:text-foreground focus-visible:outline-2 focus-visible:outline-ring"
      >
        <LayoutDashboard className="h-3 w-3" /> Add to dashboard
      </button>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent>
          {added ? (
            <>
              <DialogHeader>
                <DialogTitle>Chart added</DialogTitle>
                <DialogDescription>"{title.trim() || titleOfQuery(query)}" is on {added.name}.</DialogDescription>
              </DialogHeader>
              <DialogFooter>
                <Button variant="outline" onClick={() => setOpen(false)}>Stay here</Button>
                <Link href={`/${orgSlug}/dashboards/${added.id}`} className={buttonVariants()}>Open {added.name}</Link>
              </DialogFooter>
            </>
          ) : (
            <form onSubmit={add} className="space-y-4">
              <DialogHeader>
                <DialogTitle>Add to dashboard</DialogTitle>
                <DialogDescription>The chart keeps this Project and Query, and runs over the Dashboard's range.</DialogDescription>
              </DialogHeader>
              <div className="space-y-2">
                <Label>Dashboard</Label>
                <Select value={target} onValueChange={value => value && setTarget(value)} items={items}>
                  <SelectTrigger aria-label="Dashboard" className="w-full"><SelectValue /></SelectTrigger>
                  <SelectContent>
                    {items.map(item => <SelectItem key={item.value} value={item.value}>{item.label}</SelectItem>)}
                  </SelectContent>
                </Select>
              </div>
              {target === NEW && (
                <div className="space-y-2">
                  <Label htmlFor="new-dashboard-name">New dashboard's name</Label>
                  <Input id="new-dashboard-name" value={name} onChange={e => setName(e.target.value)} autoFocus />
                </div>
              )}
              <div className="space-y-2">
                <Label htmlFor="chart-title">Chart title</Label>
                <Input id="chart-title" value={title} onChange={e => setTitle(e.target.value)} />
              </div>
              {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
              <DialogFooter>
                <Button type="button" variant="outline" onClick={() => setOpen(false)}>Cancel</Button>
                <Button type="submit" disabled={saving}>Add chart</Button>
              </DialogFooter>
            </form>
          )}
        </DialogContent>
      </Dialog>
    </>
  )
}
