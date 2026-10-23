import { useEffect, useState, type ReactNode } from 'react'
import { bytesText, countText, exactText, type Overview } from '@/lib/overview'

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
  /** Beside the step's block, in large type, such as the ratio. */
  aside?: ReactNode
}

/** The steps of the band, in order: Received, then Stored. */
export function bandSteps(band: NonNullable<Overview['band']>): BandStep[] {
  return [
    { key: 'received', name: 'Received', bytes: band.received.bytes, logs: band.received.logs, color: 'var(--chart-1)' },
    {
      key: 'stored', name: 'Stored', bytes: band.stored.bytes, logs: band.stored.logs, color: 'var(--chart-3)',
      aside: band.ratio !== null && (
        <span className="text-2xl font-semibold tabular-nums tracking-tight" title="Received bytes over stored bytes">
          {band.ratio.toLocaleString('en', { maximumFractionDigits: 1 })}× smaller
        </span>
      ),
    },
  ]
}

/**
 * The band: what arrived and what ClickHouse holds, a row per step, each as
 * wide as its bytes, so compression shows before a number is read. It fills
 * once on first load, unless the reader prefers reduced motion.
 */
export function OverviewBand({ band }: { band: NonNullable<Overview['band']> }) {
  const steps = bandSteps(band)
  const widest = Math.max(1, ...steps.map(step => step.bytes))
  const [filled, setFilled] = useState(false)
  useEffect(() => {
    const frame = requestAnimationFrame(() => setFilled(true))
    return () => cancelAnimationFrame(frame)
  }, [])

  return (
    <div className="space-y-6">
      <ol className="space-y-5" aria-label="Received to Stored">
        {steps.map(step => {
          const share = step.bytes > 0 ? Math.max(0.5, (step.bytes / widest) * 100) : 0
          return (
            <li key={step.key} data-step={step.key} className="space-y-1.5">
              <div className="text-sm font-medium">{step.name}</div>
              <div className="flex items-center gap-4">
                <div className="h-8 flex-1">
                  <div
                    className="h-full transition-[width] duration-700 ease-out motion-reduce:transition-none"
                    style={{ width: `${filled ? share : 0}%`, background: step.color }}
                    role="img"
                    aria-label={`${step.name}: ${bytesText(step.bytes)}`}
                  />
                </div>
              </div>
              <div className="flex flex-wrap items-baseline gap-x-6 gap-y-1">
                <span className="text-2xl font-semibold tabular-nums tracking-tight" title={`${exactText(step.bytes)} bytes`}>
                  {bytesText(step.bytes)}
                </span>
                <span className="text-sm tabular-nums text-muted-foreground" title={`${exactText(step.logs)} Logs`}>
                  {countText(step.logs)} Logs
                </span>
                {step.aside}
              </div>
            </li>
          )
        })}
      </ol>

      <dl className="flex flex-wrap gap-x-8 gap-y-2 text-sm">
        <BandFact label="Sent on the wire" title={`${exactText(band.received.bytes_on_wire)} bytes`}>
          {bytesText(band.received.bytes_on_wire)} in {countText(band.received.requests)} requests
        </BandFact>
        <BandFact label="Unreadable" title={`${exactText(band.unreadable_logs)} Logs`}>
          {countText(band.unreadable_logs)} Logs
        </BandFact>
        <BandFact label="Turned away as overloaded" title={`${exactText(band.overloaded_requests)} requests`}>
          {countText(band.overloaded_requests)} requests
        </BandFact>
      </dl>
    </div>
  )
}

function BandFact({ label, title, children }: { label: string; title: string; children: ReactNode }) {
  return (
    <div className="space-y-0.5">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="tabular-nums" title={title}>{children}</dd>
    </div>
  )
}
