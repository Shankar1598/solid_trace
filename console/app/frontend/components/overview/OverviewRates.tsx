import { valueText } from '@/lib/charts/axis'
import { bytesText, rateText, secondText, type Overview, type OverviewRate } from '@/lib/overview'

/**
 * Rates by arrival time, one row per measure: the average over the range so
 * far, then the busiest second of the Projects together, with its time.
 * Today also has the last five minutes.
 */
export function OverviewRates({ rates }: { rates: NonNullable<Overview['rates']> }) {
  return (
    <dl className="divide-y text-sm">
      <RateLine label="Logs per second" rate={rates.logs} text={rateText} />
      <RateLine label="Bytes per second" rate={rates.bytes} text={bytes => `${bytesText(bytes)}/s`} />
      <RateLine label="Requests per second" rate={rates.requests} text={rateText} />
      {rates.last_five_minutes && (
        <div className="flex items-baseline justify-between gap-3 py-2 last:pb-0" data-rate="last-five-minutes">
          <dt className="text-muted-foreground">Last five minutes</dt>
          <dd className="tabular-nums">{rateText(rates.last_five_minutes.logs_per_second)} Logs per second</dd>
        </div>
      )}
    </dl>
  )
}

function RateLine({ label, rate, text }: { label: string; rate: OverviewRate; text: (value: number) => string }) {
  return (
    <div className="grid grid-cols-[1fr_auto] items-baseline gap-x-3 gap-y-0.5 py-2 first:pt-0 last:pb-0" data-rate={label}>
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="tabular-nums">
        <span className="text-base font-semibold" title={valueText(rate.average)}>{text(rate.average)}</span> average
      </dd>
      <dd className="col-span-2 text-right text-xs tabular-nums text-muted-foreground">
        {rate.peak_at ? (
          <>
            <span className="font-medium text-foreground" title={valueText(rate.peak)}>{text(rate.peak)}</span> peak at {secondText(rate.peak_at)}
          </>
        ) : (
          'no peak yet'
        )}
      </dd>
    </div>
  )
}
