// The records that the end-to-end tests sign in with and visit. Each
// Playwright worker has its own Organization, Project, admin and member, so
// flows in different workers do not see each other's Dashboards, Monitors,
// Members or Logs. Worker 0 (the only worker of a run with one) uses the
// records of `db:seed`. Worker N uses copies with the suffix -wN, which global
// setup makes. Their names are the same as the seed's, so the pages show the
// same text.

export interface Identity {
  org: string
  project: string
  admin: { email: string; password: string }
  member: { email: string; password: string }
  /** The admin's signed-in browser state that global setup saves, relative to console/. */
  adminState: string
}

export function identity(worker: number): Identity {
  const suffix = worker === 0 ? '' : `-w${worker}`
  return {
    org: `solid-trace-org${suffix}`,
    project: `solid-trace-project${suffix}`,
    admin: { email: `admin${suffix}@solidtrace.local`, password: 'password123' },
    member: { email: `member${suffix}@solidtrace.local`, password: 'password123' },
    adminState: `tmp/playwright-auth/admin${suffix}.json`,
  }
}

// Playwright sets TEST_PARALLEL_INDEX (0 to workers - 1) in each worker
// process. A worker that restarts after a failure keeps its index.
const mine = identity(Number(process.env.TEST_PARALLEL_INDEX ?? 0))

export const ADMIN = mine.admin
export const MEMBER = mine.member
export const ORG = mine.org
export const PROJECT = mine.project
export const ADMIN_STATE = mine.adminState
export const logsPath = (query = '') => `/${ORG}/projects/${PROJECT}/logs${query}`
