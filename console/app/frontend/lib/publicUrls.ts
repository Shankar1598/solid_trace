/**
 * What a blank Console URL or Ingest URL falls back to, as
 * InstanceSettingsAnswer gives it (PublicUrls): the env var, the address the
 * request reached Console at, or Console's host at port 4000.
 */
export interface UrlFallback {
  url: string
  source: 'CONSOLE_URL' | 'request' | 'PUBLIC_INGEST_URL' | 'console_url'
}

/** The hint under a URL field: what leaving it blank uses. */
export function fallbackHint(fallback: UrlFallback | null): string {
  if (!fallback) return 'Blank leaves Console without a URL for links built outside a request.'
  switch (fallback.source) {
    case 'CONSOLE_URL': return `Blank uses CONSOLE_URL: ${fallback.url}`
    case 'request': return `Blank uses the address Console was reached at: ${fallback.url}`
    case 'PUBLIC_INGEST_URL': return `Blank uses PUBLIC_INGEST_URL: ${fallback.url}`
    case 'console_url': return `Blank uses Console's host at port 4000: ${fallback.url}`
  }
}

/**
 * The scheme a Console URL must have, from the HTTPS row Console read at start
 * (its value "On" or "Off"), or null when there's no such row.
 */
export function requiredScheme(https: string | number | boolean | null | undefined): 'https' | 'http' | null {
  if (https === 'On') return 'https'
  if (https === 'Off') return 'http'
  return null
}
