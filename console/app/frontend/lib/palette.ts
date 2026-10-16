// The command palette's items: a small registry of sources. The dashboard
// layout registers its own (each Project's Logs, the pages, recent Queries
// and actions); a page adds its items while it is open, and later pages
// (Dashboards, Monitors) add sources of theirs. Each source gives its items
// for the palette's context whenever the palette opens.

export interface PaletteItem {
  id: string
  /** The group it is listed under, such as "Logs" or "Actions". */
  group: string
  label: string
  /** Dimmed after the label, and searched with it. */
  detail?: string
  /** A page to visit, or else something to do. */
  href?: string
  run?: () => void
}

export interface PaletteContext {
  orgSlug: string
  projects: { name: string; slug: string }[]
  dashboards: { id: number; name: string }[]
}

export type PaletteSource = (context: PaletteContext) => PaletteItem[]

export interface PaletteGroup {
  group: string
  items: PaletteItem[]
}

const sources = new Map<string, PaletteSource>()

/**
 * Adds a source of items, in place of any of the same name. The returned
 * function removes it, unless another has taken its name since.
 */
export function registerPaletteSource(name: string, source: PaletteSource): () => void {
  sources.set(name, source)
  return () => {
    if (sources.get(name) === source) sources.delete(name)
  }
}

/** Every source's items, by group, the groups in the order they first appear. */
export function paletteGroups(context: PaletteContext): PaletteGroup[] {
  const groups = new Map<string, PaletteItem[]>()
  for (const source of sources.values()) {
    for (const item of source(context)) {
      const items = groups.get(item.group) ?? []
      items.push(item)
      groups.set(item.group, items)
    }
  }
  return [...groups].map(([group, items]) => ({ group, items }))
}

/** The items with every word of `text` in their group, label or detail, ignoring case; groups left empty go. */
export function filterGroups(groups: PaletteGroup[], text: string): PaletteGroup[] {
  const words = text.toLowerCase().split(/\s+/).filter(Boolean)
  if (words.length === 0) return groups
  return groups
    .map(({ group, items }) => ({
      group,
      items: items.filter(item => {
        const haystack = `${group} ${item.label} ${item.detail ?? ''}`.toLowerCase()
        return words.every(word => haystack.includes(word))
      }),
    }))
    .filter(g => g.items.length > 0)
}

/** A Query on one line for the palette: its stages after a Project's source, joined by |. */
export function queryLine(query: string): string {
  const line = query.split('\n').map(l => l.trim()).filter(Boolean).join(' ')
  const stages = line.replace(/^\['[^']*'\]\s*\|\s*/, '')
  return stages === '' ? line : stages
}
