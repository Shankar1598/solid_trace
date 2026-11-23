import { router } from '@inertiajs/react'

/**
 * Saves a chart or a Text block through Inertia, which reloads the
 * Dashboard: a change to the item with an id, or a new item without one.
 * The page keeps its scroll and state, so its range and runs stay, and only
 * the item saved runs again. An empty section_id in the data is no Section.
 */
export function saveItem({ orgSlug, dashboardId, id, search, data, onSuccess, onError, onFinish }: {
  orgSlug: string
  dashboardId: number
  /** The item changed, or null for a new one. */
  id: number | null
  /** The Dashboard's URL search, so it keeps its range and filter values after saving. */
  search: string
  /** The item's fields under chart, as the Dashboard Charts controller takes them. */
  data: Parameters<typeof router.post>[1]
  onSuccess: () => void
  onError: (errors: Record<string, string>) => void
  onFinish: () => void
}) {
  const visit = { preserveScroll: true, preserveState: true, onSuccess, onError, onFinish }
  if (id !== null) router.patch(`/${orgSlug}/dashboards/${dashboardId}/charts/${id}${search}`, data, visit)
  else router.post(`/${orgSlug}/dashboards/${dashboardId}/charts${search}`, data, visit)
}
