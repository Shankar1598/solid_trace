// The Overview's Compared with other tools, as Overview::OtherTools
// (app/services/overview/other_tools.rb) answers it, and how the page writes
// its dollars.

export interface OtherToolEstimate {
  /** datadog, better_stack or axiom. */
  key: string
  name: string
  plan: string
  /** The pricing page. */
  url: string
  /** USD a month at list prices. */
  monthly_usd: number
  /** Datadog only: a Project keeps Logs longer than Datadog lists a price for, so this is the least it costs. */
  beyond_list?: boolean
  /** Axiom only: the compression its storage is priced at, ClickHouse's measured (raw bytes over stored) or 10× when nothing is stored. */
  compression?: { ratio: number; measured: boolean }
}

export interface OverviewOtherTools {
  /** The day the prices were checked, YYYY-MM-DD. */
  checked_on: string
  /** The range's volume scaled to 30 days by the seconds it has had so far; scaled is false only for 30 whole days. */
  volume: { days: number; seconds: number; scaled: boolean; logs: number; bytes: number; bytes_on_wire: number }
  tools: OtherToolEstimate[]
  egress: Omit<OtherToolEstimate, 'key' | 'beyond_list'>
  /**
   * The Spans at Datadog APM's list prices, null with no Spans: one APM host
   * taken per Project that sent Spans, each indexed at its trace Retention.
   */
  apm: (OtherToolEstimate & { per_host_usd: number; volume: { spans: number; bytes: number; hosts: number } }) | null
}

const dollars = new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD', maximumFractionDigits: 0 })
const cents = new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD', minimumFractionDigits: 2 })

/** The day the prices were checked: Oct 24, 2026. */
export function checkedText(day: string): string {
  return new Intl.DateTimeFormat('en', { year: 'numeric', month: 'short', day: 'numeric', timeZone: 'UTC' }).format(new Date(`${day}T00:00:00Z`))
}

/** Dollars, whole from $100 up, with cents below. */
export function usdText(amount: number): string {
  return (amount >= 100 ? dollars : cents).format(amount)
}
