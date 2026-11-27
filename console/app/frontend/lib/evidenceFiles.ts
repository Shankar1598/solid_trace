import type { StorageLike } from './recentQueries'

export interface EvidenceFile {
  id: string
  caption: string | null
  content_type: string
  byte_size: number
  agent_session_id: string
  uploaded_by: string | null
  created_at: string
  deleted_at: string | null
}

export const evidencePath = (slug: string, id: string) => `/${slug}/evidence/${id}`

export const evidenceBytesPath = (slug: string, id: string) => `${evidencePath(slug, id)}/file`

export const isVideo = (file: EvidenceFile) => file.content_type.startsWith('video/')

export function evidenceTitle(file: EvidenceFile): string {
  const kind = isVideo(file) ? 'Video' : 'Screenshot'
  return file.caption ? `${kind}: ${file.caption}` : kind
}

const DAY_MS = 24 * 60 * 60 * 1000

const DELETED_DAY = new Intl.DateTimeFormat('en', { year: 'numeric', month: 'short', day: 'numeric', timeZone: 'UTC' })

export function evidenceDeletedText(createdAt: string, deletedAt: string): string {
  const deleted = new Date(deletedAt)
  const days = Math.floor((deleted.getTime() - new Date(createdAt).getTime()) / DAY_MS)
  return `This evidence was deleted on ${DELETED_DAY.format(deleted)} by Evidence Retention, ${days} ${days === 1 ? 'day' : 'days'} after its upload.`
}

/**
 * Whether a click on a link to an Evidence file opens the popup: the main
 * button with no Cmd, Ctrl, Shift or Alt. Any other click keeps the
 * browser's way, such as a new tab.
 */
export function isPlainClick(e: { button: number; metaKey: boolean; ctrlKey: boolean; shiftKey: boolean; altKey: boolean; defaultPrevented: boolean }): boolean {
  return e.button === 0 && !e.metaKey && !e.ctrlKey && !e.shiftKey && !e.altKey && !e.defaultPrevented
}

/**
 * The id of the Evidence file in `ids` whose page `href` links, in the
 * Organization `slug`, or null. The link is `<console>/<slug>/evidence/<id>`,
 * as `solidtrace evidence add` prints it, on any host: a Console can have
 * more than one address.
 */
export function evidenceFileOfLink(href: string | undefined, slug: string, ids: string[]): string | null {
  if (!href) return null
  let url: URL
  try {
    url = new URL(href, 'http://console.invalid')
  } catch {
    return null
  }
  if (url.protocol !== 'https:' && url.protocol !== 'http:') return null
  const parts = url.pathname.split('/').filter(Boolean)
  const [id, evidence, org] = parts.reverse()
  return org === slug && evidence === 'evidence' && ids.includes(id) ? id : null
}

export const SPEEDS = ['0.25', '0.5', '1', '2'] as const

export type Speed = (typeof SPEEDS)[number]

const SPEED_KEY = 'solidtrace.evidenceVideo.speed'

export function readSpeed(storage: StorageLike | null): Speed {
  try {
    const kept = storage?.getItem(SPEED_KEY)
    return SPEEDS.find(speed => speed === kept) ?? '1'
  } catch {
    return '1'
  }
}

export function saveSpeed(storage: StorageLike | null, speed: Speed) {
  try {
    storage?.setItem(SPEED_KEY, speed)
  } catch {
    return
  }
}
