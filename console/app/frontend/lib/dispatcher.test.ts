import { describe, expect, it } from 'vitest'
import { dispatcherStatusLine } from './dispatcher'

describe('dispatcherStatusLine', () => {
  it('says the Dispatcher answers, with its version, when SolidTrace accepts its token', () => {
    expect(dispatcherStatusLine({ version: 'v0.3.0', token_active: true })).toEqual({
      ok: true,
      text: 'The Dispatcher answers (v0.3.0), and SolidTrace accepts its token.',
    })
  })

  it('names a development Dispatcher when it has no version', () => {
    expect(dispatcherStatusLine({ token_active: true }).text).toBe(
      'The Dispatcher answers (a development build), and SolidTrace accepts its token.',
    )
  })

  it("gives the Dispatcher's reason when SolidTrace refuses its token", () => {
    expect(dispatcherStatusLine({ version: 'v0.3.0', token_active: false, token_error: 'The Admin left Acme.' })).toEqual({
      ok: false,
      text: 'The Dispatcher answers (v0.3.0), but SolidTrace refuses its token: The Admin left Acme.',
    })
  })

  it('says how to make a new token when SolidTrace refuses it with no reason', () => {
    expect(dispatcherStatusLine({ version: 'v0.3.0', token_active: false }).text).toBe(
      "The Dispatcher answers (v0.3.0), but SolidTrace refuses its token. An Admin makes a new Dispatcher's token in Settings → API tokens & MCP.",
    )
  })

  it("shows Console's error when the Dispatcher does not answer", () => {
    expect(dispatcherStatusLine({ error: 'The Dispatcher at http://10.100.0.3:8080 did not answer in time.' })).toEqual({
      ok: false,
      text: 'The Dispatcher at http://10.100.0.3:8080 did not answer in time.',
    })
  })
})
