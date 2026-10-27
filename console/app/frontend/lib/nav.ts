// Which sidebar entry a page belongs to, from its path alone: the search is
// left out, since a Query in it can hold any text, such as "/issues".

export type NavSection = 'overview' | 'issues' | 'logs' | 'performance' | 'dashboards' | 'monitors' | 'projects' | 'settings'

// The parts of a Console URL's path after its Organization.
function pathParts(url: string): string[] {
  return url.split(/[?#]/)[0].replace(/\/+$/, '').split('/').slice(2)
}

/** The section of a Console URL under an Organization, such as `/acme/projects/shop/logs?q=…`; null for none. */
export function navSection(url: string): NavSection | null {
  const [first, , third] = pathParts(url)
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
      if (third === 'performance') return 'performance'
      return 'projects'
    default:
      return null
  }
}

/** The slug of the Project whose Performance a URL is, or null for any other page. */
export function performanceProject(url: string): string | null {
  const [first, slug, third] = pathParts(url)
  return first === 'projects' && third === 'performance' ? slug : null
}

/**
 * Where switching to the Organization `slug` lands from the page at `url`:
 * that Organization's index for the page's section (Settings meaning its
 * Organization settings), or its Issues for a page with no section.
 */
export function orgSwitchUrl(url: string, slug: string): string {
  const section = navSection(url) ?? 'issues'
  if (section === 'settings') return `/${slug}/settings/organization`
  // Performance belongs to a Project, which the other Organization lacks.
  return `/${slug}/${section === 'performance' ? 'projects' : section}`
}
