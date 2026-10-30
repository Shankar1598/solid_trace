import { test, expect, type Page } from '@playwright/test'
import { logsPath, PROJECT } from '../support/data'
import { builderRow, last24h, runBy, settled, streamRows, summaryLine } from '../support/logs'
import { watchErrors } from '../support/screens'

// A search EventStore refuses for reading too much, as the explorer shows it
// in place of results: why, the estimate against the budget, the largest range
// that fits and the busiest files, each a button that runs the narrower
// Query, and Run anyway. The dev Project is far too small to be refused, so
// the refusal is stubbed for an unforced search over more than three hours
// with no file filter, as EventStore answers it; narrower and forced runs go
// through to the dev stack.

const SEARCH = `['${PROJECT}']\n| search "Performing"`
const HOUR = 3600_000

const refused = (page: Page) => page.locator('[data-refusal]')

/** A run of the explorer or its histogram: its Query, range and whether it was forced. */
interface Asked {
  path: string
  query: string
  from: number
  to: number
  force: boolean
}

/** EventStore's refusal of a search over [from, to), with the last two hours as the largest range that fits. */
function overBudget(from: number, to: number) {
  const iso = (ms: number) => new Date(ms).toISOString().replace('.000Z', 'Z')
  return {
    message: [
      'Not run: this Query matches text (search, or contains, startswith or =~ on message), so it reads the text of every Log '
        + `that its filters on service and file keep: about 12.3 GiB in 31.2M Logs from ${iso(from)} to ${iso(to)}, `
        + 'past the 4 GiB a Query that matches text may read.',
      `The largest range that fits ends where this one does: the last 2h, from ${iso(to - 2 * HOUR)} to ${iso(to)}.`,
      'The busiest files in the range, to filter on with == or in on service and file:',
      '- service "api", file "api.log": 20.1M Logs, 8.4 GiB',
      '- service "worker", no file: 3M Logs, 858.3 MiB',
    ].join('\n'),
    busy: false,
    estimate: {
      from: iso(from), to: iso(to), text_match: true, logs: 31_200_000, bytes: 13_314_398_617,
      budget_bytes: 4_294_967_296, over_budget: true, heavy: true, forced: false,
    },
    largest_range: { from: iso(to - 2 * HOUR), to: iso(to), bytes: 4_100_000_000 },
    top_files: [
      { service: 'api', file: 'api.log', logs: 20_100_000, bytes: 9_100_000_000 },
      { service: 'worker', file: '', logs: 3_000_000, bytes: 900_000_000 },
    ],
    narrower_query: { query: `${SEARCH.split('\n')[0]}\n| where file == "api.log"\n| search "Performing"`, from: iso(from), to: iso(to) },
  }
}

/**
 * Answers `refusal` for the explorer's runs and histograms that EventStore
 * would refuse: unforced, with no file filter, over more than three hours.
 * The rest go through. Returns what was asked, in order.
 */
async function refuseLongSearches(page: Page, refusal: (from: number, to: number) => object = overBudget): Promise<Asked[]> {
  const asked: Asked[] = []
  await page.route(/\/logs\/(query|histogram)\?/, route => {
    const url = new URL(route.request().url())
    const run: Asked = {
      path: url.pathname,
      query: url.searchParams.get('query') ?? '',
      from: Date.parse(url.searchParams.get('from')!),
      to: Date.parse(url.searchParams.get('to')!),
      force: url.searchParams.get('force') === '1',
    }
    asked.push(run)
    if (run.force || run.query.includes('file ==') || run.to - run.from <= 3 * HOUR) return route.continue()
    return route.fulfill({ json: { refusal: refusal(run.from, run.to), elapsed_ms: 4 } })
  })
  return asked
}

/** Opens the explorer's Builder on the search over a fixed 24 h, and waits for its answer. */
async function openSearch(page: Page) {
  await page.goto(logsPath(`${last24h('builder')}&${new URLSearchParams({ q: SEARCH })}`))
  await settled(page)
}

test('a search that reads too much shows the refusal; a busy file filters it, and Run anyway runs it as written', async ({ page }) => {
  const errors = watchErrors(page)
  const asked = await refuseLongSearches(page)
  await openSearch(page)

  // The refusal in place of results, not an error; the histogram follows it.
  await expect(refused(page)).toContainText('Not run: this Query matches text')
  await expect(refused(page)).toContainText('Would read about 12.3 GiB in 31.2M Logs')
  await expect(refused(page)).toContainText('A Query that matches text may read 4 GiB')
  await expect(refused(page).getByRole('button', { name: /^Last 2h/ })).toBeVisible()
  await expect(refused(page).getByRole('button', { name: /^api\.log/ })).toBeVisible()
  // Logs with no file are named, but no filter keeps them.
  await expect(refused(page).getByText('no file', { exact: true })).toBeVisible()
  await expect(refused(page).getByRole('button', { name: /no file/ })).toHaveCount(0)
  await expect(refused(page).getByRole('button', { name: 'Run anyway' })).toBeVisible()
  await expect(page.getByText("Not counted: the Query wasn't run.")).toBeVisible()
  await expect(page.getByText(/The Query failed/)).toHaveCount(0)

  // A busy file: a Where chip in the Builder, and the search runs on that file.
  await runBy(page, () => refused(page).getByRole('button', { name: /^api\.log/ }).click())
  await expect(builderRow(page, 'Where').getByRole('button', { name: 'file == "api.log"', exact: true })).toBeVisible()
  await expect(refused(page)).toHaveCount(0)
  const filtered = asked.filter(a => a.path.endsWith('/logs/query')).at(-1)!
  expect(filtered.query).toContain('file == "api.log"')
  expect(filtered.query).toContain('search "Performing"')
  expect(filtered.force).toBe(false)

  // Back to the refused search, then Run anyway: the run and its histogram ask with force.
  await page.goBack()
  await settled(page)
  await expect(refused(page)).toBeVisible()
  const forced = (path: string) => page.waitForRequest(request => {
    const url = new URL(request.url())
    return url.pathname.endsWith(path) && url.searchParams.get('force') === '1' && url.searchParams.get('query') === SEARCH
  })
  const forcedRun = forced('/logs/query')
  const forcedHistogram = forced('/logs/histogram')
  await refused(page).getByRole('button', { name: 'Run anyway' }).click()
  await Promise.all([forcedRun, forcedHistogram])
  await settled(page)

  await expect(refused(page)).toHaveCount(0)
  await expect(streamRows(page).first()).toContainText(/Performing/i)
  await expect(summaryLine(page)).toContainText(/\d Logs?\b/)
  expect(errors).toEqual([])
})

test('the largest range that fits runs the search over it', async ({ page }) => {
  const errors = watchErrors(page)
  await refuseLongSearches(page)
  await openSearch(page)
  const to = Date.parse(new URL(page.url()).searchParams.get('to')!)

  await runBy(page, () => refused(page).getByRole('button', { name: /^Last 2h/ }).click())

  await expect(refused(page)).toHaveCount(0)
  const url = new URL(page.url())
  expect(url.searchParams.get('from')).toBe(new Date(to - 2 * HOUR).toISOString())
  expect(url.searchParams.get('to')).toBe(new Date(to).toISOString())
  expect(url.searchParams.get('q')).toBe(SEARCH)
  expect(errors).toEqual([])
})

test('while another large Query runs, the refusal is its message alone, without Run anyway', async ({ page }) => {
  const errors = watchErrors(page)
  await refuseLongSearches(page, (from, to) => ({
    message: 'Another large Query is running. Run them one at a time.',
    busy: true,
    estimate: { ...overBudget(from, to).estimate, bytes: 1_610_612_736, over_budget: false },
    largest_range: null,
    top_files: [],
    narrower_query: null,
  }))
  await openSearch(page)

  await expect(refused(page)).toHaveText('Another large Query is running. Run them one at a time.')
  await expect(refused(page).getByRole('button')).toHaveCount(0)
  await expect(page.getByText("Not counted: the Query wasn't run.")).toBeVisible()
  expect(errors).toEqual([])
})
