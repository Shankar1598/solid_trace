// Which sidebar entry a page belongs to, from its path alone: the search is
// left out, since a Query in it can hold any text, such as "/issues".

export type NavSection = 'overview' | 'issues' | 'logs' | 'dashboards' | 'monitors' | 'projects' | 'settings'

/** The section of a Console URL under an Organization, such as `/acme/projects/shop/logs?q=…`; null for none. */
export function navSection(url: string): NavSection | null {
  const path = url.split(/[?#]/)[0].replace(/\/+$/, '')
  const parts = path.split('/').slice(2)
  const [first, , third] = parts
  switch (first) {
    case 'overview':
    case 'issues':
    case 'logs':
    case 'dashboards':
    case 'monitors':
    case 'settings':
      return first
    case 'projects':
      if (third === 'issues') return 'issues'
      if (third === 'logs') return 'logs'
      return 'projects'
    default:
      return null
  }
}

/**
 * Where switching to the Organization `slug` lands from the page at `url`:
 * that Organization's index for the page's section (Settings meaning its
 * Organization settings), or its Issues for a page with no section.
 */
export function orgSwitchUrl(url: string, slug: string): string {
  const section = navSection(url) ?? 'issues'
  return section === 'settings' ? `/${slug}/settings/organization` : `/${slug}/${section}`
}
