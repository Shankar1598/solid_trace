import { describe, expect, it } from 'vitest'
import { dispatcherStatusLine, factorySectionShown, routeMatchText, scheduleNextText } from './dispatcher'

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

describe('factorySectionShown', () => {
  const stored = { settings: { repository: 'bitbucket.org/example/shop', folder: '', base_branch: '' }, routes: [], schedules: [] }

  it('shows nothing until the page loads the settings the first time', () => {
    expect(factorySectionShown(undefined, undefined)).toBeUndefined()
  })

  it('keeps the last settings while the page loads them again after a save, so the form stays with its errors and typed values', () => {
    expect(factorySectionShown(undefined, stored)).toBe(stored)
  })

  it('shows the settings that the page loaded last', () => {
    const saved = { settings: { repository: 'bitbucket.org/example/new', folder: 'apps/shop', base_branch: 'develop' }, routes: [], schedules: [] }
    expect(factorySectionShown(saved, stored)).toBe(saved)
    expect(factorySectionShown({ error: 'The Dispatcher does not answer' }, stored)).toEqual({ error: 'The Dispatcher does not answer' })
  })
})

describe('routeMatchText', () => {
  it('quotes a match value', () => {
    expect(routeMatchText('Shop')).toBe('“Shop”')
  })

  it('names the empty match value as the default of the connection', () => {
    expect(routeMatchText('')).toBe('Each other ticket (the default)')
  })
})

describe('scheduleNextText', () => {
  it("gives the next time of a schedule that is on, in the schedule's time zone", () => {
    const schedule = { paused: false, next_at: '2026-11-16T02:00:00Z' }
    expect(scheduleNextText({ ...schedule, time_zone: 'UTC' })).toBe('Nov 16, 02:00 UTC')
    expect(scheduleNextText({ ...schedule, time_zone: 'Asia/Kolkata' })).toBe('Nov 16, 07:30 GMT+5:30')
  })

  it('says that a paused schedule has no next time', () => {
    expect(scheduleNextText({ paused: true, next_at: null, time_zone: 'UTC' })).toBe('Paused')
  })

  it('says when a schedule that is on has no next time', () => {
    expect(scheduleNextText({ paused: false, next_at: null, time_zone: 'UTC' })).toBe('None')
  })

  it('uses UTC for a time zone that this browser does not know', () => {
    expect(scheduleNextText({ paused: false, next_at: '2026-11-16T02:00:00Z', time_zone: 'Mars/Base' })).toBe('Nov 16, 02:00 UTC')
  })
})
