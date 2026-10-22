import { router } from '@inertiajs/react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { addField, offeredFields, orderSummary } from '@/lib/logFileOrder'
import { X } from 'lucide-react'
import { useState } from 'react'

interface LogFileOrderCardProps {
  /** Where the Project is saved. */
  url: string
  /** The Project's order: null for the default, [] for time only. */
  order: string[] | null
  defaultOrder: string[]
  /** The Project's string Attributes to offer, once loaded. */
  attributes?: string[]
  canManage: boolean
}

/**
 * A Project's Log file order: the Fields each hour's Log file is sorted by
 * before time. Admins add and remove Fields and save them, or save the
 * default or time only at once; other members see it read-only.
 */
export function LogFileOrderCard({ url, order, defaultOrder, attributes = [], canManage }: LogFileOrderCardProps) {
  // null while the draft is the Project's default.
  const [draft, setDraft] = useState<string[] | null>(order)
  const [typed, setTyped] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const fields = draft ?? defaultOrder
  const changed = JSON.stringify(draft) !== JSON.stringify(order)

  const save = (next: string[] | null) => {
    setDraft(next)
    setSaving(true)
    router.patch(url, { project: { log_file_order: next } }, {
      preserveScroll: true,
      // Keep the draft beside its error; a save shows the order saved.
      preserveState: 'errors',
      onSuccess: () => setError(null),
      onError: (errors) => setError(errors.log_file_order ? String(errors.log_file_order) : 'Failed to save the Log file order'),
      onFinish: () => setSaving(false),
    })
  }

  const add = () => {
    setDraft(addField(fields, typed))
    setTyped('')
  }

  return (
    <Card>
      <form onSubmit={(e) => { e.preventDefault(); save(draft) }}>
        <CardHeader>
          <CardTitle>Log file order</CardTitle>
          <CardDescription>
            The Fields each hour's Log file is sorted by, before time, so a Query filtered on them reads less.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-3 pb-6">
          <p className="text-sm" data-testid="log-file-order">{orderSummary(draft, defaultOrder)}</p>
          {fields.length > 0 && (
            <ul className="flex flex-wrap gap-2" aria-label="Log file order Fields">
              {fields.map((field, i) => (
                <li key={`${field}-${i}`}>
                  <Badge className="font-mono">
                    {field}
                    {canManage && (
                      <button
                        type="button"
                        aria-label={`Remove ${field}`}
                        onClick={() => setDraft(fields.filter((_, j) => j !== i))}
                      >
                        <X />
                      </button>
                    )}
                  </Badge>
                </li>
              ))}
            </ul>
          )}
          {canManage && (
            <div className="space-y-2">
              <Label htmlFor="logFileOrderField">Add a Field</Label>
              <div className="flex gap-2">
                <Input
                  id="logFileOrderField"
                  list="logFileOrderFields"
                  value={typed}
                  onChange={(e) => setTyped(e.target.value)}
                  onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); add() } }}
                  placeholder="service, environment, level or an Attribute"
                  className="font-mono text-xs"
                />
                <datalist id="logFileOrderFields">
                  {offeredFields(fields, attributes).map(name => <option key={name} value={name} />)}
                </datalist>
                <Button type="button" variant="outline" onClick={add} disabled={typed.trim() === ''}>Add</Button>
              </div>
            </div>
          )}
          {error && <p className="text-sm font-medium text-destructive">{error}</p>}
          <p className="text-xs text-muted-foreground">
            A change applies to hours exported after it; Log files already written keep their order.
            It helps most for Fields that have a column of their own, which a Project's busiest Attributes get.
            Each Field added makes reads over a narrow time span read a little more.
            {!canManage && ' Only organization admins can change this.'}
          </p>
        </CardContent>
        {canManage && (
          <CardFooter className="justify-end gap-2">
            <Button type="button" variant="outline" disabled={saving} onClick={() => save(null)}>Reset to default</Button>
            <Button type="button" variant="outline" disabled={saving} onClick={() => save([])}>Time only</Button>
            <Button type="submit" disabled={saving || !changed}>{saving ? 'Saving…' : 'Save'}</Button>
          </CardFooter>
        )}
      </form>
    </Card>
  )
}
