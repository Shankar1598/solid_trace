import * as Sentry from '@sentry/react'
import type { BrowserErrors } from '@/types'
import { csrfToken } from './apiTokens'

/** Console's route that sends the frontend's envelopes on to its own Sentry (BrowserErrorsController). */
export const TUNNEL = '/browser_errors'

/**
 * Starts the frontend's Sentry SDK when Console sends its errors: uncaught
 * errors, rejected promises and what the error boundary catches go to
 * Console's Sentry, the Meta Project on an install, through TUNNEL. Without
 * config, as signed out or with no DSN, it sends nothing.
 *
 * The DSN is a stand-in: the SDK needs one to make an envelope, but Console
 * sends it with its own DSN, so the browser never holds the Meta Project's
 * key. No traces, and no sessions or client reports, only errors.
 */
export function reportBrowserErrors(config: BrowserErrors | null | undefined) {
  if (!config) return
  Sentry.init({
    dsn: `${window.location.protocol}//browser@${window.location.host}/0`,
    tunnel: TUNNEL,
    release: config.release ?? undefined,
    environment: config.environment,
    integrations: defaults => defaults.filter(integration => integration.name !== 'BrowserSession'),
    sendClientReports: false,
    // The page's CSRF token, read for each send, as Console checks it on TUNNEL.
    transport: options => Sentry.makeFetchTransport(options, (input, init) =>
      window.fetch(input, { ...init, headers: { ...init?.headers, 'X-CSRF-Token': csrfToken() } })),
  })
}
