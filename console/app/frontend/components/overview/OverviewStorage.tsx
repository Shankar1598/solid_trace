import { AlertTriangle, CheckCircle2, XCircle } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { valueText } from '@/lib/charts/axis'
import { bytesText, dayText, shareText } from '@/lib/overview'
import { diskFillsFirst, limitStatus, untilLimitText, type OverviewStorage as Storage, type StorageStatus } from '@/lib/overviewStorage'
import { cn } from '@/lib/utils'

const TONES = { ok: 'ok', warning: 'warn', error: 'error' } as const
const ICONS = { ok: CheckCircle2, warning: AlertTriangle, error: XCircle }
const WORDS = { ok: 'Within the limit', warning: 'Near the limit', error: 'Past the limit' }
const FILLS: Record<StorageStatus, string> = { ok: 'bg-primary', warning: 'bg-warning', error: 'bg-destructive' }

/**
 * The Overview's Storage, for Instance admins: the whole install's local
 * disk use as a meter against the disk limit, with a status in words and an
 * icon, then quiet lines for the free disk, the bucket, the growth and the
 * old Parquet files from before the switch to ClickHouse.
 */
export function OverviewStorage({ storage }: { storage: Storage }) {
  const status = limitStatus(storage)
  const Icon = ICONS[status]
  // From the bytes, not the rounded share, so a sliver reads as under 0.1%.
  const share = storage.disk_limit_bytes > 0 ? storage.local_bytes / storage.disk_limit_bytes : null
  const old = storage.old_files
  return (
    <div className="grid gap-x-8 gap-y-4 lg:grid-cols-2">
      <div className="space-y-2" data-storage="local">
        <div className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1">
          <p className="text-sm">
            <span className="text-2xl font-semibold tabular-nums">{bytesText(storage.local_bytes)}</span>
            <span className="text-muted-foreground"> on local disk of a {bytesText(storage.disk_limit_bytes)} limit</span>
          </p>
          <Badge variant={TONES[status]} data-storage-status={status}>
            <Icon aria-hidden />
            {WORDS[status]}{share !== null && `, ${shareText(share)}`}
          </Badge>
        </div>
        <div
          className="h-2 w-full bg-muted"
          role="meter"
          aria-label="Local disk against the disk limit"
          aria-valuemin={0}
          aria-valuemax={storage.disk_limit_bytes}
          aria-valuenow={Math.min(storage.local_bytes, storage.disk_limit_bytes)}
        >
          <div className={cn('h-full', FILLS[status])} style={{ width: `${Math.min((share ?? 0) * 100, 100)}%` }} />
        </div>
      </div>

      <dl className="space-y-2 text-sm">
        <Line label="Free disk" hook="free">
          {bytesText(storage.free_bytes)} free of {bytesText(storage.disk_bytes)} on ClickHouse&rsquo;s local disk
          {diskFillsFirst(storage) && (
            <span className="mt-1 flex items-start gap-1.5 text-warning" data-storage-notice="disk-fills-first">
              <AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0" aria-hidden />
              Less than the limit leaves room for: the disk fills before the limit moves or deletes any day.
            </span>
          )}
        </Line>
        <Line label="Bucket" hook="bucket">
          {storage.bucket ? `${bytesText(storage.bucket_bytes)} of Logs` : 'None: every Log stays on local disk'}
        </Line>
        <Line label="Growth" hook="growth">
          {bytesText(storage.growth_bytes_per_day)} a day over the last 7 days. {untilLimitText(storage)}
        </Line>
        <Line label="Old Parquet files" hook="old-files">
          {old ? (
            <>
              <span title={valueText(old.files)}>{valueText(old.files)}</span> {old.files === 1 ? 'file' : 'files'} from
              before the switch to ClickHouse, {bytesText(old.local_bytes)} of them on local disk, the newest
              of {dayText(old.newest_day)}.{' '}
              {old.passes_retention_on
                ? `The last passes Retention on ${dayText(old.passes_retention_on)}.`
                : 'Their Projects have no Retention, so they stay.'}
            </>
          ) : (
            <span className="text-muted-foreground">None left: every Log is in ClickHouse.</span>
          )}
        </Line>
      </dl>
    </div>
  )
}

function Line({ label, hook, children }: { label: string; hook: string; children: React.ReactNode }) {
  return (
    <div className="flex flex-wrap gap-x-3" data-storage={hook}>
      <dt className="w-32 shrink-0 text-muted-foreground">{label}</dt>
      <dd className="min-w-0 flex-1 tabular-nums">{children}</dd>
    </div>
  )
}
