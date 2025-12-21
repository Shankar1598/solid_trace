// Type definitions for SolidTrace application

export interface User {
  id: number
  name: string
  email: string
  created_at?: string
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
  last_seen_at: string
  updated_at: string
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
  flash: {
    notice?: string
    alert?: string
  }
  [key: string]: unknown
}
