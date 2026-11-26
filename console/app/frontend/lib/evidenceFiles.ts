import { format } from 'date-fns'
import type { StorageLike } from './recentQueries'

/** An Evidence file: a screenshot or a video that an agent uploaded to prove a PR. */
export interface EvidenceFile {
  id: string
  caption: string | null
  content_type: string
  byte_size: number
  agent_session_id: string
  /** The uploader's name, null after Console deleted the user. */
  uploaded_by: string | null
  created_at: string
  /** When Evidence Retention deleted the bytes. */
  deleted_at: string | null
}

export const evidencePath = (slug: string, id: string) => `/${slug}/evidence/${id}`

export const evidenceBytesPath = (slug: string, id: string) => `${evidencePath(slug, id)}/file`

export const isVideo = (file: EvidenceFile) => file.content_type.startsWith('video/')

/** "Screenshot: <caption>" or "Video: <caption>", as the PR body names the file. */
export function evidenceTitle(file: EvidenceFile): string {
  const kind = isVideo(file) ? 'Video' : 'Screenshot'
  return file.caption ? `${kind}: ${file.caption}` : kind
}

const DAY_MS = 24 * 60 * 60 * 1000

/**
 * The sentence of a file that Evidence Retention deleted. N is the whole
 * days from the upload to the delete: the days the file was kept, which
 * stays true after a change of the setting.
 */
export function evidenceDeletedText(file: EvidenceFile & { deleted_at: string }): string {
  const deleted = new Date(file.deleted_at)
  const days = Math.floor((deleted.getTime() - new Date(file.created_at).getTime()) / DAY_MS)
  return `This evidence was deleted on ${format(deleted, 'MMM d, yyyy')}, after ${days} ${days === 1 ? 'day' : 'days'} of Evidence Retention.`
}

export const SPEEDS = ['0.25', '0.5', '1', '2'] as const

export type Speed = (typeof SPEEDS)[number]

const SPEED_KEY = 'solidtrace.evidenceVideo.speed'

/** The viewer's last video speed, 1x when none is kept or storage throws. */
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
