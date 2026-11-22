import { describe, expect, it } from 'vitest'
import { actorText, checkText, durationText, eventDetailText, eventKindText, executionDurationText, runsSearch, stateText, stateTone, waitingOnPersonFilters } from './runs'

describe('durationText', () => {
  it('gives hours, minutes and seconds, leaving out the larger parts that are zero', () => {
    expect(durationText(1830)).toBe('30m 30s')
    expect(durationText(3720)).toBe('1h 2m')
    expect(durationText(42)).toBe('42s')
    expect(durationText(0)).toBe('0s')
  })

  it('says that an Execution with no end is still live', () => {
    expect(executionDurationText({ duration_seconds: null, live: true })).toBe('Live')
    expect(executionDurationText({ duration_seconds: null, live: false })).toBe('-')
    expect(executionDurationText({ duration_seconds: 42, live: false })).toBe('42s')
  })
})

describe('eventDetailText', () => {
  it('writes the outcome of a report in sentence case and leaves other details as they are', () => {
    expect(eventDetailText('report', 'needs_a_person')).toBe('Needs a person')
    expect(eventDetailText('report', 'done')).toBe('Done')
    expect(eventDetailText('message_accepted', 'fix_this_now')).toBe('fix_this_now')
  })
})

describe('eventKindText', () => {
  it('names each event kind in sentence case', () => {
    expect(eventKindText('execution_started')).toBe('Execution started')
    expect(eventKindText('take_over')).toBe('Take-over')
    expect(eventKindText('pr_opened')).toBe('PR opened')
    expect(eventKindText('stop')).toBe('Stop')
  })
})

describe('checkText', () => {
  it('says pass, or fail with the exit status', () => {
    expect(checkText({ passed: true, exit_status: 0 })).toBe('Pass')
    expect(checkText({ passed: false, exit_status: 2 })).toBe('Fail (exit status 2)')
    expect(checkText({ passed: false, exit_status: null })).toBe('Fail')
  })
})

describe('runsSearch', () => {
  it('keeps a span as a span, and leaves out the filters that are not set', () => {
    expect(runsSearch({ kind: 'relative', span: '30d' }, { project: null, state: null })).toBe('?range=30d')
    expect(runsSearch({ kind: 'relative', span: '90d' }, { project: 'shop', state: 'waiting_on_person' }))
      .toBe('?range=90d&project=shop&state=waiting_on_person')
  })

  it('gives from and to for a fixed range', () => {
    const range = { kind: 'absolute' as const, from: new Date('2026-10-01T00:00:00Z'), to: new Date('2026-10-02T00:00:00Z') }
    expect(runsSearch(range, { project: null, state: 'done' }))
      .toBe('?from=2026-10-01T00%3A00%3A00.000Z&to=2026-10-02T00%3A00%3A00.000Z&state=done')
  })
})

describe('waitingOnPersonFilters', () => {
  it('sets the state to waiting on a person and keeps the Project', () => {
    expect(waitingOnPersonFilters({ project: 'shop', state: null })).toEqual({ project: 'shop', state: 'waiting_on_person' })
  })

  it('clears the state when it is already waiting on a person', () => {
    expect(waitingOnPersonFilters({ project: 'shop', state: 'waiting_on_person' })).toEqual({ project: 'shop', state: null })
  })
})

describe('stateText and stateTone', () => {
  it('names each state in sentence case', () => {
    expect(stateText('waiting_on_person')).toBe('Waiting on a person')
    expect(stateText('waiting_on_account_limit')).toBe('Waiting on an account limit')
    expect(stateText('taken_over')).toBe('Taken over')
    expect(stateText('something_new')).toBe('something_new')
  })

  it('gives a wait a warning tone, a failure an error tone and done an ok tone', () => {
    expect(stateTone('waiting_on_person')).toBe('warn')
    expect(stateTone('failed')).toBe('error')
    expect(stateTone('done')).toBe('ok')
    expect(stateTone('running')).toBe('info')
    expect(stateTone('stopped')).toBe('neutral')
  })
})

describe('actorText', () => {
  it('gives the name, else the kind and id', () => {
    expect(actorText({ kind: 'person', id: '7', name: 'Ana' })).toBe('Ana')
    expect(actorText({ kind: 'schedule', id: 'nightly', name: '' })).toBe('schedule nightly')
    expect(actorText({ kind: '', id: '', name: '' })).toBe('Unknown')
  })
})
