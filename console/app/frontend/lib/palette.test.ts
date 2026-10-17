import { describe, expect, it } from 'vitest'
import { filterGroups, paletteGroups, queryLine, registerPaletteSource, type PaletteContext, type PaletteItem } from './palette'

const context: PaletteContext = { orgSlug: 'acme', projects: [{ name: 'Storefront', slug: 'storefront' }], dashboards: [], monitors: [] }
const item = (group: string, label: string, detail?: string): PaletteItem => ({ id: `${group}/${label}`, group, label, detail, href: '/' })

describe('the palette registry', () => {
  it('lists every source\'s items, by group in the order groups first appear', () => {
    const removeA = registerPaletteSource('test-a', () => [item('Logs', 'Storefront'), item('Actions', 'Toggle theme')])
    const removeB = registerPaletteSource('test-b', ctx => [item('Dashboards', `${ctx.orgSlug} overview`), item('Actions', 'Copy link')])

    const groups = paletteGroups(context).filter(g => ['Logs', 'Actions', 'Dashboards'].includes(g.group))
    expect(groups.map(g => [g.group, g.items.map(i => i.label)])).toEqual([
      ['Logs', ['Storefront']],
      ['Actions', ['Toggle theme', 'Copy link']],
      ['Dashboards', ['acme overview']],
    ])

    removeB()
    expect(paletteGroups(context).some(g => g.group === 'Dashboards')).toBe(false)
    removeA()
  })

  it('replaces a source registered again under its name, and keeps the newer when the older is removed', () => {
    const removeOld = registerPaletteSource('test-c', () => [item('Pages', 'Old')])
    const removeNew = registerPaletteSource('test-c', () => [item('Pages', 'New')])
    removeOld()

    expect(paletteGroups(context).flatMap(g => g.items).map(i => i.label)).toContain('New')
    removeNew()
    expect(paletteGroups(context).flatMap(g => g.items).map(i => i.label)).not.toContain('New')
  })
})

describe('searching the palette', () => {
  const groups = [
    { group: 'Logs', items: [item('Logs', 'Storefront', 'Logs'), item('Logs', 'Backoffice', 'Logs')] },
    { group: 'Actions', items: [item('Actions', 'Toggle theme')] },
  ]

  it('keeps items with every word typed, in the group, label or detail, any case', () => {
    expect(filterGroups(groups, 'store LOGS').flatMap(g => g.items.map(i => i.label))).toEqual(['Storefront'])
    expect(filterGroups(groups, 'theme').map(g => g.group)).toEqual(['Actions'])
    expect(filterGroups(groups, '  ')).toEqual(groups)
    expect(filterGroups(groups, 'nothing')).toEqual([])
  })
})

describe('a recent Query on one line', () => {
  it('leaves out its Project\'s source and joins its stages', () => {
    expect(queryLine("['storefront']\n| where level == \"error\"\n| summarize count() by service"))
      .toBe('where level == "error" | summarize count() by service')
    expect(queryLine("['storefront']")).toBe("['storefront']")
    expect(queryLine('logs\n  | take 5')).toBe('logs | take 5')
  })
})
