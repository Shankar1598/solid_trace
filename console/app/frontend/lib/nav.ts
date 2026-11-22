// Which sidebar entry a page belongs to, from its path alone: the search is
// left out, since a Query in it can hold any text, such as "/issues".

export type NavSection = 'overview' | 'issues' | 'logs' | 'performance' | 'dashboards' | 'monitors' | 'agent_sessions' | 'runs' | 'projects' | 'settings'

/** The sidebar's entries, in order, for the Organization `slug`. Runs is the Factory's: with no Dispatcher URL (factoryOn false) there is no Runs entry. */
export function sidebarItems(slug: string, factoryOn: boolean): { section: NavSection; label: string; href: string }[] {
  const entries: [NavSection, string, string][] = [
    ['overview', 'Overview', 'overview'],
    ['issues', 'Issues', 'issues'],
    ['logs', 'Logs', 'logs'],
    ['performance', 'Performance', 'performance'],
    ['dashboards', 'Dashboards', 'dashboards'],
    ['monitors', 'Monitors', 'monitors'],
    ['agent_sessions', 'Agent sessions', 'agent_sessions'],
    ...(factoryOn ? [['runs', 'Runs', 'runs'] as [NavSection, string, string]] : []),
    ['projects', 'Projects', 'projects'],
    ['settings', 'Settings', 'settings/organization'],
  ]
  return entries.map(([section, label, path]) => ({ section, label, href: `/${slug}/${path}` }))
}

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
    case 'performance':
    case 'dashboards':
    case 'monitors':
    case 'agent_sessions':
    case 'runs':
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

/**
 * Where switching to the Organization `slug` lands from the page at `url`:
 * that Organization's index for the page's section (Settings meaning its
 * Organization settings), or its Issues for a page with no section.
 */
export function orgSwitchUrl(url: string, slug: string): string {
  const section = navSection(url) ?? 'issues'
  if (section === 'settings') return `/${slug}/settings/organization`
  return `/${slug}/${section}`
}
