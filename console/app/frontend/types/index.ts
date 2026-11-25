// Type definitions for SolidTrace application

export interface User {
  id: number
  name: string
  email: string
  created_at?: string
  /** Whether they change the install's settings, such as email; only on current_user. */
  instance_admin?: boolean
}

export interface Organization {
  id: number
  name: string
  slug: string
}

export interface Project {
  id: number
  name: string
  slug: string
  platform?: string
  description?: string | null
  log_retention_days?: number | null
  trace_retention_days?: number | null
  created_at: string
  issues_count?: number
  keys?: ProjectKey[]
}

export interface ProjectKey {
  id: number
  public_key: string
  label?: string
  dsn: string | null
  created_at: string
}

export interface Issue {
  id: number
  number: number
  title: string
  culprit?: string
  status: 'unresolved' | 'resolved'
  kind: 'default' | 'error' | 'csp'
  events_count: number
  created_at: string
  first_seen_at: string
  last_seen_at: string
  updated_at: string
  assignee: {
    id: number
    discarded_at?: string | null
    user: User
  } | null
  project: {
    id: number
    name: string
    slug: string
  }
}

export interface Event {
  id: number
  issue_id: number
  environment?: string
  event_data: Record<string, unknown>
  created_at: string
}

export interface BreadcrumbValue {
  category: string
  data?: Record<string, unknown>
  level: 'fatal' | 'error' | 'warning' | 'info' | 'debug' | null
  message: string
  timestamp: number
  type: string | null
}

export interface Breadcrumbs {
  values: BreadcrumbValue[]
}

export interface Comment {
  id: number
  body: string
  created_at: string
  // The member who wrote it; null for a comment by Factory, whose run is
  // its Run, with the Run page's path.
  user: User | null
  run: { id: string; url: string } | null
}

export interface Integration {
  id: number
  name: string
  provider: string
  enabled: boolean
  settings: Record<string, unknown>
  created_at: string
  /** The last delivery that failed, on the Edit page only; null when none, or when a later delivery was sent. */
  last_failure?: IntegrationFailure | null
}

/** A delivery of an Integration that failed: when, why, and whether Console tries it again. */
export interface IntegrationFailure {
  at: string
  reason: string
  retried: boolean
}

// Inertia shared props
export interface SharedProps {
  current_user: User | null
  current_org: Organization | null
  /** The viewer's Organizations by name, for the sidebar's selector and the command palette; absent outside an Organization. */
  organizations?: { name: string; slug: string }[] | null
  /** The Organization's Projects, for the command palette. */
  org_projects: { name: string; slug: string }[] | null
  /** The Organization's Dashboards, for the command palette and Add to dashboard. */
  org_dashboards: { id: number; name: string }[] | null
  /** The Organization's Monitors, for the command palette. */
  org_monitors: { id: number; name: string }[] | null
  /** Whether the Dispatcher URL is set: with none, no part of the Factory that needs the Dispatcher shows. */
  factory_on?: boolean
  flash: {
    notice?: string
    alert?: string
  }
  [key: string]: unknown
}
