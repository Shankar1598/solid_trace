// The Overview's Storage section, as Overview::Storage
// (app/services/overview/storage.rb) answers it for Instance admins: the
// whole install's, every Organization's.

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
  /** Whole days until the limit at that growth; null with a bucket or no growth. */
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

/** How close the local disk is to the limit: past it, near it, or within it. */
export function limitStatus({ limit_share: share, days_until_limit: days }: OverviewStorage): StorageStatus {
  if (share !== null && share >= 1) return 'error'
  if ((share !== null && share >= NEAR_LIMIT_SHARE) || (days !== null && days < NEAR_LIMIT_DAYS)) return 'warning'
  return 'ok'
}

/** About how long until the limit, at the last 7 days' growth. */
export function untilLimitText(storage: OverviewStorage): string {
  const days = storage.days_until_limit
  if (storage.bucket) return 'Logs move to the bucket, so the local disk doesn’t fill with them.'
  if (days === null) return 'Nothing stored over the last 7 days, so no estimate.'
  if (days === 0) return 'Past the limit now: the oldest days are being deleted.'
  return `About ${days === 1 ? 'a day' : `${new Intl.NumberFormat('en').format(days)} days`} until the limit at this growth.`
}
