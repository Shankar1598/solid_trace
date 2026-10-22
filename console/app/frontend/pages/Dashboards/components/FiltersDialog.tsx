import { useMemo, useState } from 'react'
import { router } from '@inertiajs/react'
import { ArrowDown, ArrowUp, Plus, Trash2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Segmented } from '@/components/ui/segmented'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import {
  optionsOfText, textOfOptions, type Dashboard, type DashboardChart, type DashboardFilter, type FilterOptions,
} from '@/lib/dashboards/dashboard'
import { parametersOf } from '@/lib/logs/queryLexer'
import type { ProjectOption } from '../../Logs/components/BuilderPanel'

/** A filter as the dialog edits it; saved filters keep their names. */
interface Draft {
  key: number
  saved: boolean
  name: string
  label: string
  kind: DashboardFilter['kind']
  multiple: boolean
  /** Where a select's options come from. */
  from: FilterOptions['from']
  /** A fixed list's options as typed, one per line. */
  options: string
  /** A field select's Project and field. */
  projectId: number | null
  field: string
}

const KINDS = [{ value: 'select' as const, label: 'Select' }, { value: 'search' as const, label: 'Search box' }]
const SOURCES = [{ value: 'list' as const, label: 'Fixed list' }, { value: 'field' as const, label: 'From a field' }]

const draftOf = (filter: DashboardFilter, key: number): Draft => {
  const options = filter.kind === 'select' ? filter.options : null
  return {
    key,
    saved: true,
    name: filter.name,
    label: filter.label === filter.name ? '' : filter.label,
    kind: filter.kind,
    multiple: filter.kind === 'select' && filter.multiple,
    from: options?.from ?? 'list',
    options: options?.from === 'list' ? textOfOptions(options.items) : '',
    projectId: options?.from === 'field' ? options.project_id : null,
    field: options?.from === 'field' ? options.field : '',
  }
}

const optionsOf = (draft: Draft): FilterOptions => draft.from === 'field'
  ? { from: 'field', project_id: draft.projectId ?? 0, field: draft.field }
  : { from: 'list', items: optionsOfText(draft.options) }

const filterOf = (draft: Draft): DashboardFilter => draft.kind === 'search'
  ? { name: draft.name.trim(), label: draft.label, kind: 'search' }
  : { name: draft.name.trim(), label: draft.label, kind: 'select', multiple: draft.multiple, options: optionsOf(draft) }

const FIELDS: Record<string, string> = {
  '': 'The filter', name: 'Name', label: 'Label', kind: 'Kind', multiple: 'Several values', options: 'Options',
  'options.project_id': 'Project', 'options.field': 'Field',
}

/** A refusal of one filter's field, as `name` or `options[1].value`, as a sentence. */
function errorText(field: string, message: string): string {
  const option = /^options\[(\d+)\](?:\.(\w+))?$/.exec(field)
  const subject = option ? `Option ${Number(option[1]) + 1}${option[2] ? `'s ${option[2]}` : ''}` : FIELDS[field] ?? field
  return `${subject} ${message}.`
}

/**
 * Adds, removes, reorders and edits a Dashboard's filters, and saves them as
 * one list. A Query names a filter as `$name`, so a saved filter's name can't
 * change; removing a filter that charts name asks first.
 */
export function FiltersDialog({ dashboard, projects, orgSlug, search, onClose }: {
  dashboard: Dashboard & { charts: DashboardChart[] }
  /** The Organization's Projects, one of which a select's field is read from. */
  projects: ProjectOption[]
  orgSlug: string
  /** The Dashboard's URL search, so it keeps its range and filter values after saving. */
  search: string
  onClose: () => void
}) {
  const [drafts, setDrafts] = useState<Draft[]>(() => dashboard.filters.map(draftOf))
  const [nextKey, setNextKey] = useState(dashboard.filters.length)
  const [removing, setRemoving] = useState<number | null>(null)
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [saving, setSaving] = useState(false)
  const projectItems = useMemo(() => projects.map(p => ({ value: String(p.id), label: p.name })), [projects])

  const change = (key: number, patch: Partial<Draft>) => setDrafts(ds => ds.map(d => (d.key === key ? { ...d, ...patch } : d)))
  const move = (index: number, by: number) => setDrafts(ds => {
    const next = [...ds]
    const [draft] = next.splice(index, 1)
    next.splice(index + by, 0, draft)
    return next
  })
  const add = () => {
    setDrafts(ds => [...ds, {
      key: nextKey, saved: false, name: '', label: '', kind: 'select', multiple: false,
      from: 'list', options: '', projectId: projects[0]?.id ?? null, field: '',
    }])
    setNextKey(k => k + 1)
  }
  const remove = (key: number) => {
    setDrafts(ds => ds.filter(d => d.key !== key))
    setRemoving(null)
  }
  const chartsNaming = (name: string) => dashboard.charts.filter(c => parametersOf(c.query).includes(name.trim()))
  const askOrRemove = (draft: Draft) => (chartsNaming(draft.name).length > 0 ? setRemoving(draft.key) : remove(draft.key))

  const save = (e: React.FormEvent) => {
    e.preventDefault()
    setSaving(true)
    router.patch(`/${orgSlug}/dashboards/${dashboard.id}${search}`, { dashboard: { filters: drafts.map(filterOf) } }, {
      preserveScroll: true,
      preserveState: true,
      onSuccess: onClose,
      onError: setErrors,
      onFinish: () => setSaving(false),
    })
  }

  // Errors are keyed by a filter's place in the list saved: filters[1].name.
  const errorsOf = (index: number) => Object.entries(errors).flatMap(([key, message]) => {
    const prefix = `filters[${index}]`
    if (key !== prefix && !key.startsWith(`${prefix}.`)) return []
    return [errorText(key.slice(prefix.length + 1), String(message))]
  })

  return (
    <Dialog open onOpenChange={open => !open && onClose()}>
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-2xl">
        <form onSubmit={save} className="space-y-4">
          <DialogHeader>
            <DialogTitle>Filters</DialogTitle>
            <DialogDescription>
              A chart follows a filter by naming it in its Query, as <code>where level == $level</code> or <code>search $q</code>.
              A filter's name can't change once saved.
            </DialogDescription>
          </DialogHeader>

          {drafts.length === 0 && <p className="border border-dashed p-6 text-center text-muted-foreground">No filters yet.</p>}
          {drafts.map((draft, index) => {
            const n = index + 1
            const naming = removing === draft.key ? chartsNaming(draft.name) : []
            return (
              <fieldset key={draft.key} className="space-y-2 border p-3" aria-label={`Filter ${n}`}>
                <div className="flex flex-wrap items-center gap-2">
                  <Input
                    aria-label={`Filter ${n} name`} className="h-7 w-40 font-mono" placeholder="name" value={draft.name}
                    disabled={draft.saved} onChange={e => change(draft.key, { name: e.target.value })}
                  />
                  <Input
                    aria-label={`Filter ${n} label`} className="h-7 w-44" placeholder={draft.name || 'Label'} value={draft.label}
                    onChange={e => change(draft.key, { label: e.target.value })}
                  />
                  <Segmented size="sm" label={`Filter ${n} kind`} value={draft.kind} options={KINDS} onChange={kind => change(draft.key, { kind })} />
                  <div className="ml-auto flex gap-1">
                    <Button type="button" variant="ghost" size="icon" className="h-7 w-7" aria-label={`Move filter ${n} up`}
                      disabled={index === 0} onClick={() => move(index, -1)}><ArrowUp className="h-3.5 w-3.5" /></Button>
                    <Button type="button" variant="ghost" size="icon" className="h-7 w-7" aria-label={`Move filter ${n} down`}
                      disabled={index === drafts.length - 1} onClick={() => move(index, 1)}><ArrowDown className="h-3.5 w-3.5" /></Button>
                    <Button type="button" variant="ghost" size="icon" className="h-7 w-7" aria-label={`Remove filter ${n}`}
                      onClick={() => askOrRemove(draft)}><Trash2 className="h-3.5 w-3.5" /></Button>
                  </div>
                </div>
                {draft.kind === 'select' && (
                  <>
                    <div className="flex flex-wrap items-center gap-3">
                      <Segmented
                        size="sm" label={`Filter ${n} options from`} value={draft.from} options={SOURCES}
                        onChange={from => change(draft.key, { from })}
                      />
                      <label className="flex items-center gap-2">
                        <Checkbox checked={draft.multiple} onCheckedChange={checked => change(draft.key, { multiple: checked === true })} />
                        Several values
                      </label>
                    </div>
                    {draft.from === 'list' ? (
                      <>
                        <Textarea
                          aria-label={`Filter ${n} options`} rows={3} className="font-mono" value={draft.options}
                          placeholder={'error\nWarning = warn'} onChange={e => change(draft.key, { options: e.target.value })}
                        />
                        <p className="text-muted-foreground">Options, one per line: a value, or a label and its value as <code>Label = value</code>.</p>
                      </>
                    ) : (
                      <>
                        <div className="flex flex-wrap items-center gap-2">
                          <Select
                            value={draft.projectId === null ? null : String(draft.projectId)} items={projectItems}
                            onValueChange={value => value && change(draft.key, { projectId: Number(value) })}
                          >
                            <SelectTrigger size="sm" aria-label={`Filter ${n} Project`} className="min-w-44"><SelectValue placeholder="Project" /></SelectTrigger>
                            <SelectContent>
                              {projectItems.map(p => <SelectItem key={p.value} value={p.value}>{p.label}</SelectItem>)}
                            </SelectContent>
                          </Select>
                          <Input
                            aria-label={`Filter ${n} field`} className="h-7 w-56 font-mono" placeholder="service" value={draft.field}
                            onChange={e => change(draft.key, { field: e.target.value })}
                          />
                        </div>
                        <p className="text-muted-foreground">Options: the field&apos;s 100 most common values in the Project, over the range the Dashboard shows.</p>
                      </>
                    )}
                  </>
                )}
                {naming.length > 0 && (
                  <div className="flex flex-wrap items-center gap-2 border border-destructive/40 p-2" role="alert">
                    <span className="flex-1">
                      {naming.map(c => c.title).join(', ')} {naming.length === 1 ? 'names' : 'name'} <code>${draft.name.trim()}</code> and
                      will run it as All.
                    </span>
                    <Button type="button" size="sm" variant="destructive" onClick={() => remove(draft.key)}>Remove filter</Button>
                    <Button type="button" size="sm" variant="outline" onClick={() => setRemoving(null)}>Keep</Button>
                  </div>
                )}
                {errorsOf(index).map(text => <p key={text} className="text-destructive">{text}</p>)}
              </fieldset>
            )
          })}
          {errors.filters && <p className="text-destructive">Filters {String(errors.filters)}.</p>}

          <DialogFooter className="sm:justify-between">
            <Button type="button" variant="outline" onClick={add}><Plus className="h-3.5 w-3.5" /> Add filter</Button>
            <div className="flex gap-2">
              <Button type="button" variant="outline" onClick={onClose}>Cancel</Button>
              <Button type="submit" disabled={saving}>Save</Button>
            </div>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
