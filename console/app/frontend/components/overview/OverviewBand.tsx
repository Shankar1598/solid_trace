import { useEffect, useState, type ReactNode } from 'react'
import { statText, valueText } from '@/lib/charts/axis'
import { INK, bytesText, type Overview } from '@/lib/overview'

/**
 * One step of the band: its name, its bytes and Logs, and its colour. A step
 * is as wide as its bytes, on one linear scale for every step.
 */
export interface BandStep {
  key: string
  name: string
  bytes: number
  logs: number
  /** The step's fill, a colour token. */
  color: string
  /** Hatched slices right after the step's block, on the same scale, such as what was dropped before it. */
  slices?: BandSlice[]
}

/** A hatched slice of the band: Logs dropped between two steps. */
export interface BandSlice {
  key: string
  label: string
  bytes: number
  /** The hatch's colour, a colour token. */
  color: string
}

/** The hatches of the dropped slices: unreadable is a shipper's fault, the Log transform's drops a filter's. */
const UNREADABLE = 'var(--warning)'
const DROPPED_BY_TRANSFORM = 'var(--muted-foreground)'

/**
 * The steps of the band, in order: Received, Kept, then Stored. The Logs
 * dropped between Received and Kept, unreadable or by the Log transform,
 * follow Kept as hatched slices. Their bytes aren't counted, so each is as
 * wide as its share of the Logs received is of Received's bytes.
 */
export function bandSteps(band: NonNullable<Overview['band']>): BandStep[] {
  const { received } = band
  const droppedBytes = (logs: number) => (received.logs > 0 ? (Math.min(logs, received.logs) / received.logs) * received.bytes : 0)
  return [
    { key: 'received', name: 'Received', bytes: received.bytes, logs: received.logs, color: INK.light },
    {
      key: 'kept', name: 'Kept', bytes: band.kept.bytes, logs: band.kept.logs, color: INK.mid,
      slices: [
        { key: 'unreadable', label: `${valueText(band.unreadable_logs)} unreadable`, bytes: droppedBytes(band.unreadable_logs), color: UNREADABLE },
        {
          key: 'dropped_by_transform', label: `${valueText(band.dropped_by_transform)} dropped by the Log transform`,
          bytes: droppedBytes(band.dropped_by_transform), color: DROPPED_BY_TRANSFORM,
        },
      ].filter(slice => slice.bytes > 0),
    },
    { key: 'stored', name: 'Stored', bytes: band.stored.bytes, logs: band.stored.logs, color: INK.full },
  ]
}

function hatch(color: string): string {
  return `repeating-linear-gradient(135deg, ${color} 0 2px, transparent 2px 6px)`
}

/**
 * The band: what arrived, what Log processing kept and what ClickHouse
 * holds, a row per step, each as wide as its bytes, so drops and compression
 * show before a number is read, with the compression ratio beside them as
 * the page's one large number. It fills once on first load, unless the
 * reader prefers reduced motion.
 */
export function OverviewBand({ band }: { band: NonNullable<Overview['band']> }) {
  const steps = bandSteps(band)
  const widest = Math.max(1, ...steps.map(step => step.bytes + (step.slices ?? []).reduce((sum, s) => sum + s.bytes, 0)))
  const width = (bytes: number) => (bytes > 0 ? Math.max(0.5, (bytes / widest) * 100) : 0)
  const [filled, setFilled] = useState(false)
  useEffect(() => {
    const frame = requestAnimationFrame(() => setFilled(true))
    return () => cancelAnimationFrame(frame)
  }, [])
  const fill = 'h-full shrink-0 transition-[width] duration-700 ease-out motion-reduce:transition-none'

  return (
    <div className="space-y-3">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center">
        <ol className="min-w-0 flex-1 space-y-2" aria-label="Received to Kept to Stored">
          {steps.map(step => (
            <li key={step.key} data-step={step.key} className="grid grid-cols-[4rem_minmax(0,1fr)_6.5rem] items-center gap-3">
              <div className="text-sm font-medium">{step.name}</div>
              <div className="flex h-5">
                <div
                  className={fill}
                  style={{ width: `${filled ? width(step.bytes) : 0}%`, background: step.color }}
                  role="img"
                  aria-label={`${step.name}: ${bytesText(step.bytes)}`}
                />
                {step.slices?.map(slice => (
                  <div
                    key={slice.key}
                    data-slice={slice.key}
                    className={`${fill} border-y border-r`}
                    style={{ width: `${filled ? width(slice.bytes) : 0}%`, background: hatch(slice.color), borderColor: slice.color }}
                    role="img"
                    aria-label={`Dropped: ${slice.label} Logs`}
                    title={`Dropped: ${slice.label} Logs, as wide as their share of the Logs received`}
                  />
                ))}
              </div>
              <div className="text-right leading-tight">
                <div className="text-base font-semibold tabular-nums tracking-tight" title={`${valueText(step.bytes)} bytes`}>
                  {bytesText(step.bytes)}
                </div>
                <div className="text-xs tabular-nums text-muted-foreground" title={`${valueText(step.logs)} Logs`}>
                  {statText(step.logs)} Logs
                </div>
              </div>
            </li>
          ))}
        </ol>
        {band.ratio !== null && (
          <div className="shrink-0 sm:w-36 sm:border-l sm:pl-4" data-band="ratio">
            <div className="text-4xl font-semibold tabular-nums tracking-tight" title="Kept bytes over stored bytes">
              {band.ratio.toLocaleString('en', { maximumFractionDigits: 1 })}×<span className="sr-only"> smaller</span>
            </div>
            <div className="text-xs text-muted-foreground" aria-hidden>smaller, kept over stored</div>
          </div>
        )}
      </div>

      <dl className="flex flex-wrap gap-x-6 gap-y-1 border-t pt-2 text-xs">
        <BandFact label="Sent on the wire" title={`${valueText(band.received.bytes_on_wire)} bytes`}>
          {bytesText(band.received.bytes_on_wire)} in {statText(band.received.requests)} requests
        </BandFact>
        <BandFact label="Unreadable" title={`${valueText(band.unreadable_logs)} Logs`} hatch={UNREADABLE}>
          {statText(band.unreadable_logs)} Logs
        </BandFact>
        <BandFact label="Dropped by the Log transform" title={`${valueText(band.dropped_by_transform)} Logs`} hatch={DROPPED_BY_TRANSFORM}>
          {statText(band.dropped_by_transform)} Logs
        </BandFact>
        <BandFact label="Turned away as overloaded" title={`${valueText(band.overloaded_requests)} requests`}>
          {statText(band.overloaded_requests)} requests
        </BandFact>
      </dl>
    </div>
  )
}

function BandFact({ label, title, hatch: color, children }: { label: string; title: string; hatch?: string; children: ReactNode }) {
  return (
    <div className="flex items-center gap-1.5">
      <dt className="flex items-center gap-1.5 text-muted-foreground">
        {color && <span className="h-2.5 w-2.5 shrink-0 border" style={{ background: hatch(color), borderColor: color }} aria-hidden />}
        {label}
      </dt>
      <dd className="tabular-nums" title={title}>{children}</dd>
    </div>
  )
}
