import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { TelemetryGetStarted } from './GetStarted'

const html = (hasKey: boolean) => renderToStaticMarkup(createElement(TelemetryGetStarted, {
  captureSetupPath: '/acme/settings/agent_sessions/setup#telemetry',
  hasKey,
  orgSlug: 'acme',
  projectSlug: 'agent-sessions',
}))

describe("the empty Agent Project's Logs page", () => {
  it('links to step 5 of the Capture setup page', () => {
    expect(html(true)).toContain('href="/acme/settings/agent_sessions/setup#telemetry"')
  })

  it('says the settings have the key when the Agent Project has one', () => {
    expect(html(true)).toContain('the Ingest URL and the key filled in')
    expect(html(true)).not.toContain('no key yet')
  })

  it('says an Admin makes the key on the Agent Project\'s page first when it has none', () => {
    const page = html(false)
    expect(page).toContain('no key yet')
    expect(page).toContain('href="/acme/projects/agent-sessions"')
    expect(page).toContain('&lt;key&gt;')
    expect(page).not.toContain('the Ingest URL and the key filled in')
  })
})
