import type { SidebarGroupId } from './nav'
import type { StorageLike } from './recentQueries'

const keyOf = (group: SidebarGroupId) => `solidtrace.sidebarGroup.${group}`

export function groupOpen(storage: StorageLike | null, group: SidebarGroupId, onChildPage: boolean): boolean {
  if (onChildPage) return true
  try {
    return storage?.getItem(keyOf(group)) === 'open'
  } catch {
    return false
  }
}

export function saveGroupOpen(storage: StorageLike | null, group: SidebarGroupId, open: boolean) {
  try {
    storage?.setItem(keyOf(group), open ? 'open' : 'closed')
  } catch {
    return
  }
}
