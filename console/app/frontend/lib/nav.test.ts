import { describe, expect, it } from 'vitest'
import { navSection } from './nav'

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
