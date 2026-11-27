// The Review records pages: the newest Review record of each PR, and one
// record with its findings. A record is what an agent wrote (open-pr's
// record.md, version 2), with the words of PR commenters in it, so the
// pages show it as text only (the PR body as Markdown, with the rules of a
// Text block), and read each part with care: a field can be missing.

/** A Review record as EventStore lists it: its place, its PR and its counts. */
export type ReviewRecordRow = {
  /** The PR, or the Agent session id of a record with no PR. */
  key: string
  id: string
  session_id: string
  path: string
  version: number
  offset: number
  stored_at: string
  /** "" when no PR opened; local:<path> for a PR file in local mode. */
  pr: string
  round: number
  title: string
  findings: number
  fixed: number
  missed: number
  confirmations_not_given: number
}

/** A row of the list: the newest record of a PR, and how many records of it are older. */
export type ListedReviewRecord = ReviewRecordRow & { earlier: number }

/** The Review record of an Agent session, for the link to its page: the one the server stored last of the session's own. */
export type SessionReviewRecord = { id: string; pr: string; round: number }

export type ReviewRound = { id: string; session_id: string; path: string; version: number; offset: number; round: number; stored_at: string }

export type RecordEvent = {
  type: string
  run?: number | null
  decision?: string
  why?: string
  tradeoffs?: string
  approved_by?: string
  verdict?: string
  pushed_back?: boolean | null
  said?: string
  asks?: string
  found?: string
  changed?: string[] | null
  files?: string[] | null
  commit?: unknown
  source?: string
}

export type RecordFinding = {
  id: string
  run?: number | null
  after_run?: number | null
  found_by?: { kind?: string; name?: string } | null
  kind?: string | null
  severity?: string | null
  category?: string | null
  file?: string | null
  line?: number | null
  title?: string | null
  scenario?: string | null
  events?: RecordEvent[] | null
  outcome?: string
  confidence?: string
}

export type RecordReview = {
  run: number
  block_run?: number | null
  skill?: string | null
  trigger?: string | null
  stage?: string | null
  step?: string | null
  skipped?: string | null
  reviewers?: { name?: string; model?: string; effort?: string }[] | null
  verifier?: { model?: string; effort?: string } | null
}

export type RecordConfirmation = { id: string; run?: number; reviewer?: string; ask?: string; given?: boolean | null }

export type Story = { started_from?: string; changes?: string[]; reviews?: string[]; after_review?: string[]; sessions?: string[] }

export type ReviewRecord = {
  version: number
  pr?: string | null
  /** The PR body in Markdown. Records from plugin 0.24 and earlier do not have it. */
  pr_body?: string | null
  round?: number
  sessions?: { id: string; title?: string }[] | null
  story?: Story | null
  reviews?: RecordReview[] | null
  findings?: RecordFinding[] | null
  confirmations?: RecordConfirmation[] | null
}

/** The link of a PR: only a web address, never another kind of URL that an agent wrote. */
export function prHref(pr: string): string | null {
  try {
    const url = new URL(pr)
    return url.protocol === 'https:' || url.protocol === 'http:' ? url.href : null
  } catch {
    return null
  }
}

/** The page of the newest record of a PR (its key), or of one record (its id). */
export function recordPath(listPath: string, which: { key: string } | { id: string }): string {
  const search = new URLSearchParams('key' in which ? { key: which.key } : { id: which.id })
  return `${listPath}/record?${search}`
}

/** The page of an Agent session's own Review record, in the Organization `slug`: that round, not the PR's newest. */
export function sessionRecordPath(slug: string, record: SessionReviewRecord): string {
  return recordPath(`/${slug}/review_records`, { id: record.id })
}

/** Who found a finding, as one short text: "reviewer tests", "person", "agent verifier". */
export function foundByText(finding: RecordFinding): string {
  const by = finding.found_by
  if (!by?.kind) return 'unknown'
  return by.name ? `${by.kind} ${by.name}` : by.kind
}

/** Whether the reviews missed a finding: a person, an agent or a test run found it. */
export function missedByTheReviews(finding: RecordFinding): boolean {
  return ['person', 'agent', 'test'].includes(finding.found_by?.kind ?? '')
}

/**
 * What one event of a finding decided, in a few words; null for the event
 * that found it. "trivial", "verifier pushed back", "person asks fix",
 * "fixed".
 */
export function decisionText(event: RecordEvent): string | null {
  switch (event.type) {
    case 'found':
      return null
    case 'refound':
      return event.run != null ? `found again in run ${event.run}` : 'found again'
    case 'triage':
      return event.decision || 'triage'
    case 'verify':
      if (event.pushed_back) return 'verifier pushed back'
      return event.verdict ? `verifier ${event.verdict.toLowerCase()}` : 'verified'
    case 'person':
      return event.asks ? `person asks ${event.asks}` : 'person'
    case 'agent':
      return event.changed?.length ? 'agent changed it' : 'agent answered'
    case 'fix':
      return 'fixed'
    default:
      return event.type
  }
}

/** The decisions on a finding, in the order of its events. */
export function decisionsOf(finding: RecordFinding): string[] {
  return (finding.events ?? []).map(decisionText).filter((text): text is string => text !== null)
}

/** The words of an event that the page shows under its finding: why, what the person said, what the agent found. */
export function eventWords(event: RecordEvent): string[] {
  const words: string[] = []
  if (event.said) words.push(`“${event.said}”`)
  if (event.why) words.push(event.why)
  if (event.tradeoffs) words.push(event.tradeoffs)
  if (event.found) words.push(event.found)
  const files = event.files ?? event.changed
  if (files?.length) words.push(files.join(', '))
  return words
}

/** A confirmation's state: given, not given, or not known (null). */
export function givenText(given: boolean | null | undefined): string {
  if (given === true) return 'Given'
  if (given === false) return 'Not given'
  return 'Not known'
}

/**
 * Whether a run has no data: no block_run, skill, stage, step, reviewers,
 * verifier or skip. Records made before review record skipped a findings
 * block that could not be read have such runs. A run of a block that gives
 * only its run number has data: its block_run.
 */
export function runHasNoData(review: RecordReview): boolean {
  return review.block_run == null && !review.skill && !review.stage && !review.step && !review.skipped && !review.reviewers?.length && !review.verifier
}

/** The reviewers of a run, as "name (model, effort)" each. */
export function reviewersText(review: RecordReview): string {
  return (review.reviewers ?? [])
    .map(r => [r.model, r.effort].filter(Boolean).length ? `${r.name ?? '?'} (${[r.model, r.effort].filter(Boolean).join(', ')})` : r.name ?? '?')
    .join(', ')
}
