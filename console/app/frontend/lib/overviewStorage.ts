// The Overview's Storage section, as Overview::Storage
// (app/services/overview/storage.rb) answers it for Instance admins: the
// whole install's, every Organization's.
import { bytesText } from './overview'

export interface OverviewStorage {
  whole_install: true
  /** What the disk limit counts: ClickHouse's Logs on local disk and the old files' local copies. */
  local_bytes: number
  disk_limit_bytes: number
  /** local_bytes over the limit; null with no limit. */
  limit_share: number | null
  disk_bytes: number
  free_bytes: number
  bucket: boolean
  bucket_bytes: number
  /** The bytes of a day's Logs, over the last 7 whole days. */
  growth_bytes_per_day: number
  /** The local disk's Logs at steady state: each Project's average a day kept for its Retention, local_cache_hours with a bucket. */
  projected_local_bytes: number
  /** Whether the local disk is and stays within the limit at steady state. */
  stays_under_limit: boolean
  /** Whole days until the limit at that growth; null when it stays under the limit, or with no growth. */
  days_until_limit: number | null
  old_files: {
    files: number
    local_bytes: number
    newest_day: string
    /** The day Retention deletes the last of them; null when none of their Projects has one. */
    passes_retention_on: string | null
  } | null
}

export type StorageStatus = 'ok' | 'warning' | 'error'

/** Under this many days to the limit is a warning. */
export const NEAR_LIMIT_DAYS = 14
/** Over this share of the limit is a warning. */
export const NEAR_LIMIT_SHARE = 0.8

/**
 * How close the local disk is to the limit: past it, near it, or within it.
 * Near needs a day count: a disk that stays under the limit at steady state
 * is within it however full.
 */
export function limitStatus({ limit_share: share, days_until_limit: days }: OverviewStorage): StorageStatus {
  if (share !== null && share >= 1) return 'error'
  if (days !== null && (days < NEAR_LIMIT_DAYS || (share !== null && share >= NEAR_LIMIT_SHARE))) return 'warning'
  return 'ok'
}

/** Whether the disk has less free than the limit leaves room for, so it fills before the limit bites. */
export function diskFillsFirst(storage: OverviewStorage): boolean {
  return storage.disk_limit_bytes > 0 && storage.free_bytes < storage.disk_limit_bytes - storage.local_bytes
}

/** Whether the disk stays under the limit at steady state, or about how long until it, at the last 7 days' growth. */
export function untilLimitText(storage: OverviewStorage): string {
  const days = storage.days_until_limit
  const steady = bytesText(storage.projected_local_bytes)
  if (storage.stays_under_limit) {
    return storage.bucket
      ? `Stays under the limit: about ${steady} at steady state, as Logs move to the bucket.`
      : `Stays under the limit: about ${steady} at steady state, as Retention deletes about as much as arrives.`
  }
  if (days === 0) return 'Past the limit now: the oldest days are being deleted.'
  if (days === null) return 'Nothing stored over the last 7 days, so no estimate.'
  const when = days > 365 ? 'Over a year' : `About ${days === 1 ? 'a day' : `${new Intl.NumberFormat('en').format(days)} days`}`
  return `${when} until the limit at this growth; steady state would be about ${steady}.`
}
