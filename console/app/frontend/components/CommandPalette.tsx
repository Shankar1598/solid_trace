import { createContext, useContext, useEffect, useMemo, useState } from 'react'
import { Combobox } from '@base-ui/react/combobox'
import { router, usePage } from '@inertiajs/react'
import { Dialog, DialogContent, DialogTitle } from '@/components/ui/dialog'
import { filterGroups, paletteGroups, queryLine, registerPaletteSource, type PaletteContext, type PaletteGroup, type PaletteItem } from '@/lib/palette'
import { browserStorage, latestQueries } from '@/lib/recentQueries'
import { toggleTheme } from '@/lib/theme'
import type { SharedProps } from '@/types'
import { copyText } from './copyText'
import { ShortcutSheet } from './ShortcutSheet'
import { useShortcut } from './useShortcut'

// The layout's own items: recent Queries, each Project's Logs and the pages.
registerPaletteSource('recent-queries', ({ orgSlug, projects }) => {
  if (!orgSlug) return []
  const names = new Map(projects.map(p => [p.slug, p.name]))
  return latestQueries(browserStorage(), orgSlug, projects.map(p => p.slug)).map(r => ({
    id: `recent/${r.project}/${r.at}`,
    group: 'Recent Queries',
    label: queryLine(r.query),
    detail: names.get(r.project),
    href: `/${orgSlug}/projects/${r.project}/logs${r.search}`,
  }))
})

registerPaletteSource('logs', ({ orgSlug, projects }) => projects.map(p => ({
  id: `logs/${p.slug}`, group: 'Logs', label: p.name, detail: 'Logs', href: `/${orgSlug}/projects/${p.slug}/logs`,
})))

registerPaletteSource('pages', ({ orgSlug }) => (orgSlug ? [
  { id: 'page/issues', group: 'Pages', label: 'Issues', href: `/${orgSlug}/issues` },
  { id: 'page/projects', group: 'Pages', label: 'Projects', href: `/${orgSlug}/projects` },
  { id: 'page/settings', group: 'Pages', label: 'Settings', href: `/${orgSlug}/settings/organization` },
] : []))

const OpenPalette = createContext<() => void>(() => {})

/** Opens the command palette, as Cmd/Ctrl+K does. */
export function usePaletteOpener() {
  return useContext(OpenPalette)
}

/**
 * The command palette (Cmd/Ctrl+K) and the shortcuts sheet (?), for every
 * page of the dashboard layout. Its items come from the palette registry.
 */
export function CommandPalette({ children }: { children: React.ReactNode }) {
  const { props } = usePage<SharedProps>()
  const [open, setOpen] = useState(false)
  const [sheet, setSheet] = useState(false)
  useShortcut('palette', () => setOpen(o => !o))
  useShortcut('help', () => setSheet(true))

  useEffect(() => registerPaletteSource('actions', () => [
    { id: 'action/theme', group: 'Actions', label: 'Toggle theme', run: toggleTheme },
    { id: 'action/link', group: 'Actions', label: 'Copy link', detail: 'to this page', run: () => copyText(window.location.href, 'Link') },
    { id: 'action/shortcuts', group: 'Actions', label: 'Show shortcuts', detail: '?', run: () => setSheet(true) },
  ]), [])

  const context: PaletteContext = { orgSlug: props.current_org?.slug ?? '', projects: props.org_projects ?? [] }
  return (
    <OpenPalette.Provider value={() => setOpen(true)}>
      {children}
      <PaletteDialog open={open} onOpenChange={setOpen} context={context} />
      <ShortcutSheet open={sheet} onOpenChange={setSheet} />
    </OpenPalette.Provider>
  )
}

function PaletteDialog({ open, onOpenChange, context }: { open: boolean; onOpenChange: (open: boolean) => void; context: PaletteContext }) {
  const [text, setText] = useState('')
  // Read on opening: recent Queries and a page's items change between openings.
  const groups = useMemo(() => (open ? paletteGroups(context) : []), [open])
  const shown = filterGroups(groups, text)

  const close = () => {
    onOpenChange(false)
    setText('')
  }
  const choose = (item: PaletteItem | null) => {
    if (!item) return
    close()
    if (item.href) router.visit(item.href)
    else item.run?.()
  }

  return (
    <Dialog open={open} onOpenChange={o => (o ? onOpenChange(true) : close())}>
      <DialogContent showCloseButton={false} className="top-[15%] translate-y-0 gap-0 p-0 sm:max-w-lg">
        <DialogTitle className="sr-only">Command palette</DialogTitle>
        <Combobox.Root<PaletteItem>
          inline
          items={shown}
          filter={null}
          inputValue={text}
          onInputValueChange={setText}
          value={null}
          onValueChange={choose}
          itemToStringLabel={item => item.label}
          autoHighlight
        >
          <Combobox.Input
            aria-label="Search the palette"
            placeholder="Jump to a Project's Logs, a page, a recent Query or an action…"
            className="h-10 w-full border-b bg-transparent px-3 text-sm outline-none placeholder:text-muted-foreground"
          />
          <Combobox.Empty className="p-4 text-center text-xs text-muted-foreground empty:p-0">Nothing matches.</Combobox.Empty>
          <Combobox.List className="max-h-80 overflow-y-auto p-1 data-empty:p-0">
            {(group: PaletteGroup) => (
              <Combobox.Group key={group.group} items={group.items}>
                <Combobox.GroupLabel className="px-2 pt-2 pb-1 text-[11px] text-muted-foreground">{group.group}</Combobox.GroupLabel>
                <Combobox.Collection>
                  {(item: PaletteItem) => (
                    <Combobox.Item
                      key={item.id}
                      value={item}
                      className="flex cursor-pointer items-center gap-3 px-2 py-1.5 text-xs outline-none data-highlighted:bg-accent data-highlighted:text-accent-foreground"
                    >
                      <span className="min-w-0 truncate font-medium">{item.label}</span>
                      {item.detail && <span className="ml-auto shrink-0 text-muted-foreground">{item.detail}</span>}
                    </Combobox.Item>
                  )}
                </Combobox.Collection>
              </Combobox.Group>
            )}
          </Combobox.List>
        </Combobox.Root>
      </DialogContent>
    </Dialog>
  )
}
