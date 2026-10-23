import { bytesText, exactText, rateText, secondText, type Overview, type OverviewRate } from '@/lib/overview'

/**
 * Rates by arrival time, one quiet line per measure: the average over the
 * range so far, then the busiest second of the Projects together, with its
 * time. Today also has the last five minutes.
 */
export function OverviewRates({ rates }: { rates: NonNullable<Overview['rates']> }) {
  return (
    <dl className="space-y-2 text-sm">
      <RateLine label="Logs per second" rate={rates.logs} text={rateText} />
      <RateLine label="Bytes per second" rate={rates.bytes} text={bytes => `${bytesText(bytes)}/s`} />
      <RateLine label="Requests per second" rate={rates.requests} text={rateText} />
      {rates.last_five_minutes && (
        <div className="flex flex-wrap gap-x-3" data-rate="last-five-minutes">
          <dt className="w-44 shrink-0 text-muted-foreground">Last five minutes</dt>
          <dd className="tabular-nums">{rateText(rates.last_five_minutes.logs_per_second)} Logs per second</dd>
        </div>
      )}
    </dl>
  )
}

function RateLine({ label, rate, text }: { label: string; rate: OverviewRate; text: (value: number) => string }) {
  return (
    <div className="flex flex-wrap gap-x-3" data-rate={label}>
      <dt className="w-44 shrink-0 text-muted-foreground">{label}</dt>
      <dd className="tabular-nums">
        <span title={exactText(rate.average)}>{text(rate.average)}</span> average
        <span className="mx-2 text-muted-foreground">·</span>
        {rate.peak_at ? (
          <>
            <span className="font-medium" title={exactText(rate.peak)}>{text(rate.peak)}</span> peak at {secondText(rate.peak_at)}
          </>
        ) : (
          <span className="text-muted-foreground">no peak yet</span>
        )}
      </dd>
    </div>
  )
}
