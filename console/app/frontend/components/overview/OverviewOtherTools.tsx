import { ExternalLink } from 'lucide-react'
import { statText, valueText } from '@/lib/charts/axis'
import { bytesText } from '@/lib/overview'
import { checkedText, usdText, type OtherToolEstimate, type OverviewOtherTools as OtherTools } from '@/lib/overviewOtherTools'

/**
 * What the range's volume would cost a month at other tools' list prices,
 * one quiet line each with its plan and pricing page, then the egress to
 * send it there. The volume they price is named above them.
 */
export function OverviewOtherTools({ otherTools }: { otherTools: OtherTools }) {
  const { volume } = otherTools
  return (
    <div className="space-y-4">
      <p className="text-sm" data-other-tools="volume">
        A month of{' '}
        <span className="tabular-nums" title={`${valueText(volume.logs)} Logs`}>{statText(volume.logs)}</span> Logs,{' '}
        <span className="tabular-nums" title={`${valueText(volume.bytes)} bytes`}>{bytesText(volume.bytes)}</span> received
        {volume.scaled && (
          <span className="text-muted-foreground" data-other-tools="scaled">
            {' '}({volume.days === 1 ? 'today' : `the last ${volume.days} days`} so far, scaled to 30 days)
          </span>
        )}
      </p>
      <dl className="space-y-3 text-sm">
        {otherTools.tools.map(tool => <EstimateLine key={tool.key} id={tool.key} estimate={tool} />)}
        <EstimateLine id="egress" estimate={otherTools.egress} detail={`for ${bytesText(volume.bytes_on_wire)} on the wire`} />
      </dl>
      <p className="text-xs text-muted-foreground" data-other-tools="checked-on">
        An estimate at list prices on {checkedText(otherTools.checked_on)}, before discounts, at each Project's Retention.
        It isn't a bill, and this install's own cost isn't in it.
      </p>
    </div>
  )
}

function EstimateLine({ id, estimate, detail }: {
  id: string
  estimate: Omit<OtherToolEstimate, 'key'>
  detail?: string
}) {
  return (
    <div className="flex flex-wrap items-baseline gap-x-3" data-other-tool={id}>
      <dt className="w-full shrink-0 font-medium sm:w-44">{estimate.name}</dt>
      <dd className="flex min-w-0 flex-1 flex-wrap items-baseline gap-x-2">
        <span className="text-lg font-semibold tabular-nums" title={`$${estimate.monthly_usd.toFixed(2)}`}>
          {estimate.beyond_list && 'at least '}{usdText(estimate.monthly_usd)}
        </span>
        <span className="text-muted-foreground">a month · {estimate.plan}{detail && `, ${detail}`}</span>
        <a
          href={estimate.url}
          target="_blank"
          rel="noreferrer"
          className="inline-flex items-center gap-1 text-muted-foreground underline-offset-4 hover:text-foreground hover:underline"
        >
          Pricing<ExternalLink className="h-3 w-3" aria-hidden />
        </a>
        {estimate.beyond_list && (
          <span className="basis-full text-xs text-muted-foreground">
            A Project keeps Logs longer than the 30 days Datadog lists a price for, so it's priced at 30 days'.
          </span>
        )}
      </dd>
    </div>
  )
}
