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
}

export interface OverviewOtherTools {
  /** The day the prices were checked, YYYY-MM-DD. */
  checked_on: string
  /** The range's volume scaled to 30 days by the seconds it has had so far; scaled is false only for 30 whole days. */
  volume: { days: number; seconds: number; scaled: boolean; logs: number; bytes: number; bytes_on_wire: number }
  tools: OtherToolEstimate[]
  egress: Omit<OtherToolEstimate, 'key' | 'beyond_list'>
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
