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
  log_retention_days?: number | null
  created_at: string
  issues_count?: number
  keys?: ProjectKey[]
}

export interface ProjectKey {
  id: number
  public_key: string
  label?: string
  dsn: string
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
  user: User
}

export interface Integration {
  id: number
  name: string
  provider: string
  enabled: boolean
  settings: Record<string, unknown>
  created_at: string
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
  flash: {
    notice?: string
    alert?: string
  }
  [key: string]: unknown
}
