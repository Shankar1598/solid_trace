// The source of the fixture bundle public/vite/assets/crash-test.js, which
// BrowserSourceMapsTest un-minifies. Build it again with:
// bunx esbuild test/fixtures/files/browser_source_maps/app/frontend/crash.ts --bundle --minify --sourcemap --format=esm --outfile=test/fixtures/files/browser_source_maps/public/vite/assets/crash-test.js
export function renderChart(rows: number[]) {
  const total = rows.reduce((sum, row) => sum + row, 0)
  if (total > 10) {
    throw new Error('the chart has too many rows')
  }
  return total
}

export const shout = (value: string) => {
  if (value.length === 0) {
    throw new TypeError('nothing to shout')
  }
  return value.toUpperCase()
}
