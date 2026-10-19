import { describe, expect, it } from 'vitest'
import { navSection, orgSwitchUrl } from './nav'

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

  it('is Projects for the list, a new Project and a Project\'s page', () => {
    expect(navSection('/acme/projects')).toBe('projects')
    expect(navSection('/acme/projects/new')).toBe('projects')
    expect(navSection('/acme/projects/shop?tab=keys')).toBe('projects')
  })

  it('is Settings under settings, and none elsewhere', () => {
    expect(navSection('/acme/settings/organization')).toBe('settings')
    expect(navSection('/acme/mentions')).toBeNull()
    expect(navSection('/')).toBeNull()
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

  it('is the other Organization\'s settings from any Settings page', () => {
    expect(orgSwitchUrl('/acme/settings/organization_users', 'meta')).toBe('/meta/settings/organization')
    expect(orgSwitchUrl('/acme/settings/integrations/3/edit', 'meta')).toBe('/meta/settings/organization')
  })

  it('is the other Organization\'s Issues from a page with no section', () => {
    expect(orgSwitchUrl('/acme/mentions', 'meta')).toBe('/meta/issues')
    expect(orgSwitchUrl('/', 'meta')).toBe('/meta/issues')
  })
})
