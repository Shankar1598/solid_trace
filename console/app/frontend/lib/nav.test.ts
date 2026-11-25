import { describe, expect, it } from 'vitest'
import { navSection, orgSwitchUrl, sidebarItems, type SidebarGroup } from './nav'

describe('the sidebar entry of a page', () => {
  it('is Issues for the list and each Issue', () => {
    expect(navSection('/acme/issues')).toBe('issues')
    expect(navSection('/acme/issues?status=open')).toBe('issues')
    expect(navSection('/acme/projects/shop/issues/12')).toBe('issues')
  })

  it('is Logs for the Logs entry and each Project\'s explorer, whatever its Query holds', () => {
    expect(navSection('/acme/logs')).toBe('logs')
    expect(navSection('/acme/projects/shop/logs?range=1h&q=where+path+%3D%3D+"/issues/"')).toBe('logs')
    expect(navSection('/acme/projects/shop/logs/?q=/dashboards')).toBe('logs')
  })

  it('is Dashboards for the list and each Dashboard, and only them', () => {
    expect(navSection('/acme/dashboards')).toBe('dashboards')
    expect(navSection('/acme/dashboards/4?range=6h')).toBe('dashboards')
    expect(navSection('/acme/projects/dashboards')).toBe('projects')
  })

  it('is Monitors for the list, a new Monitor and each Monitor, and only them', () => {
    expect(navSection('/acme/monitors')).toBe('monitors')
    expect(navSection('/acme/monitors/new?query=%2Fdashboards')).toBe('monitors')
    expect(navSection('/acme/monitors/4/edit')).toBe('monitors')
    expect(navSection('/acme/projects/monitors')).toBe('projects')
    expect(navSection('/acme/projects/shop/logs?q=/monitors')).toBe('logs')
  })

  it('is Agent sessions for the list, and not for the settings page of Agent sessions', () => {
    expect(navSection('/acme/agent_sessions')).toBe('agent_sessions')
    expect(navSection('/acme/agent_sessions?range=30d&run=none')).toBe('agent_sessions')
    expect(navSection('/acme/settings/agent_sessions')).toBe('settings')
    expect(orgSwitchUrl('/acme/agent_sessions?person=3', 'meta')).toBe('/meta/agent_sessions')
  })

  it('is Review records for the list and each record, whatever its PR holds', () => {
    expect(navSection('/acme/review_records')).toBe('review_records')
    expect(navSection('/acme/review_records/record?key=https%3A%2F%2Fgithub.com%2Facme%2Fshop%2Fpull%2F7')).toBe('review_records')
    expect(orgSwitchUrl('/acme/review_records/record?id=x', 'meta')).toBe('/meta/review_records')
  })

  it('is Agent sessions or Review records alone on their pages, not the other', () => {
    expect(navSection('/acme/agent_sessions/abc')).toBe('agent_sessions')
    expect(navSection('/acme/review_records')).toBe('review_records')
    expect(navSection('/acme/review_records/record?id=x')).toBe('review_records')
  })

  it('is Runs for the Runs page with its filters', () => {
    expect(navSection('/acme/runs')).toBe('runs')
    expect(navSection('/acme/runs?state=waiting_on_person&project=shop')).toBe('runs')
    expect(orgSwitchUrl('/acme/runs?project=shop', 'meta')).toBe('/meta/runs')
  })

  it('is Projects for the list, a new Project and a Project\'s page', () => {
    expect(navSection('/acme/projects')).toBe('projects')
    expect(navSection('/acme/projects/new')).toBe('projects')
    expect(navSection('/acme/projects/shop?tab=keys')).toBe('projects')
  })

  it('is Performance for the Organization\'s, a Project\'s and its Transactions, whatever their names hold', () => {
    expect(navSection('/acme/performance')).toBe('performance')
    expect(navSection('/acme/projects/shop/performance?range=7d')).toBe('performance')
    expect(navSection('/acme/projects/shop/performance/transaction?name=GET+/issues')).toBe('performance')
  })

  it('is Settings under settings, and none elsewhere', () => {
    expect(navSection('/acme/settings/organization')).toBe('settings')
    expect(navSection('/acme/mentions')).toBeNull()
    expect(navSection('/')).toBeNull()
  })
})

describe('the Overview', () => {
  it('is its own sidebar entry, whatever its range', () => {
    expect(navSection('/acme/overview')).toBe('overview')
    expect(navSection('/acme/overview?range=7d')).toBe('overview')
  })

  it('switches to the other Organization\'s Overview, on Today', () => {
    expect(orgSwitchUrl('/acme/overview?range=30d', 'meta')).toBe('/meta/overview')
  })
})

describe('where switching Organization lands', () => {
  it('is the other Organization\'s Issues from the Issues list, an Issue or a Project\'s Issues', () => {
    expect(orgSwitchUrl('/acme/issues?status=open', 'meta')).toBe('/meta/issues')
    expect(orgSwitchUrl('/acme/projects/shop/issues/12', 'meta')).toBe('/meta/issues')
  })

  it('is the other Organization\'s Logs from the Logs entry or a Project\'s explorer', () => {
    expect(orgSwitchUrl('/acme/logs', 'meta')).toBe('/meta/logs')
    expect(orgSwitchUrl('/acme/projects/shop/logs?q=/dashboards', 'meta')).toBe('/meta/logs')
  })

  it('is the other Organization\'s Dashboards or Monitors from one of them', () => {
    expect(orgSwitchUrl('/acme/dashboards/4?range=6h', 'meta')).toBe('/meta/dashboards')
    expect(orgSwitchUrl('/acme/monitors/4/edit', 'meta')).toBe('/meta/monitors')
  })

  it('is the other Organization\'s Projects from a Project\'s page', () => {
    expect(orgSwitchUrl('/acme/projects/shop?tab=keys', 'meta')).toBe('/meta/projects')
    expect(orgSwitchUrl('/acme/projects/new', 'meta')).toBe('/meta/projects')
  })

  it('is the other Organization\'s Performance from a Project\'s Performance', () => {
    expect(orgSwitchUrl('/acme/projects/shop/performance?range=7d', 'meta')).toBe('/meta/performance')
  })

  it('is the other Organization\'s settings from any Settings page', () => {
    expect(orgSwitchUrl('/acme/settings/organization_users', 'meta')).toBe('/meta/settings/organization')
    expect(orgSwitchUrl('/acme/settings/integrations/3/edit', 'meta')).toBe('/meta/settings/organization')
  })

  it('is the other Organization\'s Issues from a page with no section', () => {
    expect(orgSwitchUrl('/acme/mentions', 'meta')).toBe('/meta/issues')
    expect(orgSwitchUrl('/', 'meta')).toBe('/meta/issues')
  })
})

describe('the sidebar entries', () => {
  const labels = (factoryOn: boolean) => sidebarItems('acme', factoryOn).map(e => e.label)
  const codingAgents = () => sidebarItems('acme', false).find((e): e is SidebarGroup => 'children' in e)!

  it('have no Runs entry on an install with no Dispatcher URL', () => {
    expect(labels(false)).toEqual(['Overview', 'Issues', 'Logs', 'Performance', 'Dashboards', 'Monitors', 'Coding agents', 'Projects', 'Settings'])
  })

  it('have the Runs entry after Coding agents when the Factory is on', () => {
    expect(labels(true).slice(6, 9)).toEqual(['Coding agents', 'Runs', 'Projects'])
    expect(sidebarItems('acme', true).find(e => e.label === 'Runs')).toEqual({ section: 'runs', label: 'Runs', href: '/acme/runs' })
  })

  it('group Agent sessions and Review records under Coding agents, and nowhere else', () => {
    expect(codingAgents().children).toEqual([
      { section: 'agent_sessions', label: 'Agent sessions', href: '/acme/agent_sessions' },
      { section: 'review_records', label: 'Review records', href: '/acme/review_records' },
    ])
    expect(labels(true)).not.toContain('Agent sessions')
    expect(labels(true)).not.toContain('Review records')
  })

  it('light the group\'s child of each of its pages', () => {
    const children = codingAgents().children
    const lit = (url: string) => children.filter(c => c.section === navSection(url)).map(c => c.label)
    expect(lit('/acme/agent_sessions?range=30d')).toEqual(['Agent sessions'])
    expect(lit('/acme/agent_sessions/abc')).toEqual(['Agent sessions'])
    expect(lit('/acme/review_records')).toEqual(['Review records'])
    expect(lit('/acme/review_records/record?key=https%3A%2F%2Fgithub.com%2Facme%2Fshop%2Fpull%2F7')).toEqual(['Review records'])
    expect(lit('/acme/issues')).toEqual([])
    expect(lit('/acme/settings/agent_sessions')).toEqual([])
  })
})
