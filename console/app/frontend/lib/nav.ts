// Which sidebar entry a page belongs to, from its path alone: the search is
// left out, since a Query in it can hold any text, such as "/issues".

export type NavSection = 'issues' | 'logs' | 'dashboards' | 'projects' | 'settings'

/** The section of a Console URL under an Organization, such as `/acme/projects/shop/logs?q=…`; null for none. */
export function navSection(url: string): NavSection | null {
  const path = url.split(/[?#]/)[0].replace(/\/+$/, '')
  const parts = path.split('/').slice(2)
  const [first, , third] = parts
  switch (first) {
    case 'issues':
    case 'logs':
    case 'dashboards':
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
