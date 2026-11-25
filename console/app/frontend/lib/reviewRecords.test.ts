import { describe, expect, it } from 'vitest'
import { decisionsOf, eventWords, foundByText, givenText, missedByTheReviews, prHref, recordPath, reviewersText } from './reviewRecords'

describe('the link of a PR', () => {
  it('is the PR\'s web address', () => {
    expect(prHref('https://github.com/acme/shop/pull/7')).toBe('https://github.com/acme/shop/pull/7')
    expect(prHref('http://git.example.com/acme/shop/pull-requests/7')).toBe('http://git.example.com/acme/shop/pull-requests/7')
  })

  it('is none for a PR file in local mode, a script URL or text', () => {
    expect(prHref('local:/work/shop/.git/solidtrace-pr/cart.md')).toBeNull()
    expect(prHref('javascript:alert(1)')).toBeNull()
    expect(prHref('data:text/html,<b>x</b>')).toBeNull()
    expect(prHref('no PR')).toBeNull()
    expect(prHref('')).toBeNull()
  })
})

describe('the page of a record', () => {
  it('names the PR by its key, or one record by its id', () => {
    expect(recordPath('/acme/review_records', { key: 'https://github.com/acme/shop/pull/7' }))
      .toBe('/acme/review_records/record?key=https%3A%2F%2Fgithub.com%2Facme%2Fshop%2Fpull%2F7')
    expect(recordPath('/acme/review_records', { id: '6f1e3a5c-2b4d-4f6a-8c0e-3d5b7f9a1c4e' }))
      .toBe('/acme/review_records/record?id=6f1e3a5c-2b4d-4f6a-8c0e-3d5b7f9a1c4e')
  })
})

describe('the decisions on a finding', () => {
  it('are its events in order, without the one that found it', () => {
    expect(decisionsOf({
      id: '3.F8', events: [
        { type: 'found' },
        { type: 'triage', decision: 'trivial' },
        { type: 'verify', verdict: 'CONFIRMED', pushed_back: true },
        { type: 'person', said: 'Fix it anyway', asks: 'fix' },
        { type: 'fix', files: ['otlp.go'] },
      ],
    })).toEqual(['trivial', 'verifier pushed back', 'person asks fix', 'fixed'])
  })

  it('name a verdict with no push back, a refound run and what the agent did', () => {
    expect(decisionsOf({
      id: 'P1', events: [
        { type: 'verify', verdict: 'REFUTED', pushed_back: false },
        { type: 'refound', run: 4 },
        { type: 'agent', found: 'only partly verified', changed: ['bin/otel-dev'] },
        { type: 'agent', found: 'it was fine', changed: [] },
      ],
    })).toEqual(['verifier refuted', 'found again in run 4', 'agent changed it', 'agent answered'])
  })

  it('are none for a finding with no events', () => {
    expect(decisionsOf({ id: 'T1-1' })).toEqual([])
    expect(decisionsOf({ id: 'T1-2', events: null })).toEqual([])
  })
})

describe('a finding\'s finder', () => {
  it('names the kind and the name', () => {
    expect(foundByText({ id: 'F1', found_by: { kind: 'reviewer', name: 'tests' } })).toBe('reviewer tests')
    expect(foundByText({ id: 'P1', found_by: { kind: 'person' } })).toBe('person')
    expect(foundByText({ id: 'X' })).toBe('unknown')
  })

  it('missed the reviews when a person, an agent or a test run found it', () => {
    expect(['person', 'agent', 'test', 'reviewer', undefined].map(kind => missedByTheReviews({ id: 'F', found_by: { kind } })))
      .toEqual([true, true, true, false, false])
  })
})

describe('the words of an event', () => {
  it('quote the person, and give the why, what the agent found and the files', () => {
    expect(eventWords({ type: 'person', said: 'Leave it', asks: 'ignore' })).toEqual(['“Leave it”'])
    expect(eventWords({ type: 'triage', decision: 'dispute', why: 'The caller checks it' })).toEqual(['The caller checks it'])
    expect(eventWords({ type: 'agent', found: 'Partly verified', changed: ['a.go', 'b.go'] })).toEqual(['Partly verified', 'a.go, b.go'])
    expect(eventWords({ type: 'found' })).toEqual([])
  })
})

describe('a confirmation and a run', () => {
  it('say whether the confirmation was given', () => {
    expect([true, false, null, undefined].map(givenText)).toEqual(['Given', 'Not given', 'Not known', 'Not known'])
  })

  it('name each reviewer with its model and effort', () => {
    expect(reviewersText({ run: 1, reviewers: [{ name: 'bugs-and-spec', model: 'opus', effort: 'xhigh' }, { name: 'code-quality', model: 'sonnet' }, { name: 'tests' }] }))
      .toBe('bugs-and-spec (opus, xhigh), code-quality (sonnet), tests')
    expect(reviewersText({ run: 2, reviewers: null })).toBe('')
  })
})
