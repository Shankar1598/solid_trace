import { useState } from 'react'
import { Markdown } from '@/components/Markdown'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { MAX_TEXT, type DashboardSection, type TextBlock } from '@/lib/dashboards/dashboard'
import { saveItem } from './saveItem'
import { SectionSelect } from './SectionSelect'

/**
 * Adds a Text block, or edits one: its Markdown in a textarea with a
 * preview beside it, its optional title, and its Section when the Dashboard
 * has any. Saving posts through Inertia, which reloads the Dashboard; a block
 * put in another Section goes under that Section's charts.
 */
export function TextBlockEditor({ block, sectionId = null, sections, orgSlug, dashboardId, search, onClose }: {
  /** The Text block edited, or null for a new one. */
  block: TextBlock | null
  /** The Section a new block goes in, or null for none. */
  sectionId?: number | null
  sections: DashboardSection[]
  orgSlug: string
  dashboardId: number
  /** The Dashboard's URL search, so it keeps its range and filter values after saving. */
  search: string
  onClose: () => void
}) {
  const [text, setText] = useState(block?.text ?? '')
  const [title, setTitle] = useState(block?.title ?? '')
  const [section, setSection] = useState<number | null>(block ? block.section_id : sectionId)
  const [saving, setSaving] = useState(false)
  const [errors, setErrors] = useState<Record<string, string>>({})

  const save = () => {
    setSaving(true)
    setErrors({})
    // An empty section_id is no Section.
    const data = { chart: { viz: 'text', text, title: title.trim(), section_id: section ?? '' } }
    saveItem({
      orgSlug, dashboardId, id: block?.id ?? null, search, data, onSuccess: onClose, onError: setErrors, onFinish: () => setSaving(false),
    })
  }

  return (
    <Dialog open onOpenChange={open => !open && onClose()}>
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-4xl">
        <DialogHeader>
          <DialogTitle>{block ? 'Edit text' : 'Add text'}</DialogTitle>
        </DialogHeader>

        <div className={sections.length > 0 ? 'grid gap-3 sm:grid-cols-[1fr_auto]' : 'grid gap-3'}>
          <div className="space-y-1">
            <Label htmlFor="text-title">Title</Label>
            <Input id="text-title" value={title} placeholder="None" maxLength={200} onChange={e => setTitle(e.target.value)} />
            {errors.title && <p className="text-xs text-destructive">Title {errors.title}</p>}
          </div>
          {sections.length > 0 && <SectionSelect sections={sections} value={section} error={errors.section} onChange={setSection} />}
        </div>

        <div className="grid gap-3 sm:grid-cols-2">
          <div className="space-y-1">
            <Label htmlFor="text-markdown">Markdown</Label>
            <Textarea
              id="text-markdown"
              className="h-72 resize-none font-mono text-xs"
              value={text}
              maxLength={MAX_TEXT}
              placeholder={'## What this Dashboard shows\n\n- **Bold**, `code`, [links](https://example.com) and tables'}
              onChange={e => setText(e.target.value)}
              autoFocus
            />
            <p className="text-[11px] text-muted-foreground">
              Headings, lists, bold, code, links and tables. No HTML; an image shows as its link. Links open in a new tab.
            </p>
            {errors.text && <p className="text-xs text-destructive">Text {errors.text}</p>}
          </div>
          <div className="space-y-1">
            <Label>Preview</Label>
            <div className="h-72 overflow-auto border p-3" data-text-preview>
              {text.trim() ? <Markdown text={text} /> : <p className="text-xs text-muted-foreground">The text shows here as the Dashboard draws it.</p>}
            </div>
          </div>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={onClose}>Cancel</Button>
          <Button onClick={save} disabled={saving || !text.trim()}>{block ? 'Save text' : 'Add text'}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
