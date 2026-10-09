// The records `db:seed` creates, which the end-to-end tests sign in with and visit.
export const ADMIN = { email: 'admin@solidtrace.local', password: 'password123' }
export const MEMBER = { email: 'member@solidtrace.local', password: 'password123' }
export const ORG = 'solid-trace-org'
export const PROJECT = 'solid-trace-project'
export const logsPath = (query = '') => `/${ORG}/projects/${PROJECT}/logs${query}`
