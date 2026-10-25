import { expect, test, type Page } from '@playwright/test'
import { ORG, PROJECT } from '../support/data'
import { shoot, useTheme, watchErrors } from '../support/screens'

// Every page at phone width: none may scroll sideways or put a control past
// the screen's right edge. Runs only in the phone projects.

test.beforeEach(async ({ page }, info) => {
  test.skip(!info.project.name.includes('phone'), 'phone only')
  await useTheme(page, info)
})

// Elements whose box runs past the viewport's right edge, skipping ones
// inside a box that scrolls sideways on purpose (tables, code).
async function offscreen(page: Page) {
  return page.evaluate(() => {
    const width = document.documentElement.clientWidth
    const scrollsAcross = (el: Element | null): boolean => {
      for (let p = el?.parentElement; p && p !== document.body; p = p.parentElement) {
        const s = getComputedStyle(p)
        if ((s.overflowX === 'auto' || s.overflowX === 'scroll' || s.overflowX === 'hidden') && p.clientWidth < p.scrollWidth + 1) {
          if (p.getBoundingClientRect().right <= width + 1) return true
        }
      }
      return false
    }
    const found: string[] = []
    for (const el of Array.from(document.body.querySelectorAll('*'))) {
      const r = el.getBoundingClientRect()
      if (r.width === 0 || r.height === 0) continue
      if (getComputedStyle(el).visibility === 'hidden') continue
      if (r.right > width + 1 && !scrollsAcross(el)) {
        const name = `${el.tagName.toLowerCase()}${el.id ? '#' + el.id : ''}.${String(el.className).split(' ').slice(0, 4).join('.')}`
        found.push(`${name} right=${Math.round(r.right)} "${(el.textContent ?? '').trim().slice(0, 40)}"`)
      }
    }
    return { scroll: document.documentElement.scrollWidth - width, found: found.slice(0, 15) }
  })
}

async function firstHref(page: Page, path: string, pattern: RegExp) {
  await page.goto(path)
  await page.waitForLoadState('networkidle').catch(() => {})
  const hrefs = await page.locator('a[href]').evaluateAll(as => as.map(a => (a as HTMLAnchorElement).getAttribute('href') ?? ''))
  return hrefs.find(h => pattern.test(h))
}

const P = `/${ORG}/projects/${PROJECT}`
const fixed: [string, string][] = [
  ['overview', `/${ORG}/overview`],
  ['projects', `/${ORG}/projects`],
  ['project', P],
  ['project-new', `/${ORG}/projects/new`],
  ['logs', `${P}/logs`],
  ['log-transform', `${P}/log_transform`],
  ['issues', `/${ORG}/issues`],
  ['dashboards', `/${ORG}/dashboards`],
  ['monitors', `/${ORG}/monitors`],
  ['monitor-new', `/${ORG}/monitors/new`],
  ['settings-organization', `/${ORG}/settings/organization`],
  ['settings-members', `/${ORG}/settings/organization_users`],
  ['settings-integrations', `/${ORG}/settings/integrations`],
  ['settings-integration-new', `/${ORG}/settings/integrations/new`],
  ['settings-user', `/${ORG}/settings/user`],
  ['settings-api-tokens', `/${ORG}/settings/api_tokens`],
  ['settings-email', `/${ORG}/settings/email`],
  ['settings-instance', `/${ORG}/settings/instance`],
  ['organization-new', `/organizations/new`],
]
for (const [name, path] of fixed) {
  test(`phone ${name}`, async ({ page }) => {
    const errors = watchErrors(page)
    await page.goto(path)
    await page.waitForLoadState('networkidle').catch(() => {})
    await shoot(page, `phone-${name}`, { fullPage: true })
    const o = await offscreen(page)
    console.log(name, JSON.stringify(o, null, 1))
    expect.soft(o.scroll, 'page scrolls sideways').toBeLessThanOrEqual(1)
    expect.soft(o.found, 'elements past the right edge').toEqual([])
    expect.soft(errors).toEqual([])
  })
}

const discovered: [string, string, RegExp][] = [
  ['issue', `/${ORG}/issues`, new RegExp(`/projects/[^/]+/issues/\\d+`)],
  ['dashboard', `/${ORG}/dashboards`, new RegExp(`/${ORG}/dashboards/[^/?]+$`)],
  ['monitor', `/${ORG}/monitors`, new RegExp(`/${ORG}/monitors/\\d+$`)],
  ['integration-edit', `/${ORG}/settings/integrations`, /\/integrations\/\d+\/edit$/],
]
for (const [name, index, pattern] of discovered) {
  test(`phone ${name}`, async ({ page }) => {
    const href = await firstHref(page, index, pattern)
    test.skip(!href, `no ${name} to open`)
    const errors = watchErrors(page)
    await page.goto(href!)
    await page.waitForLoadState('networkidle').catch(() => {})
    await shoot(page, `phone-${name}`, { fullPage: true })
    const o = await offscreen(page)
    console.log(name, href, JSON.stringify(o, null, 1))
    expect.soft(o.scroll, 'page scrolls sideways').toBeLessThanOrEqual(1)
    expect.soft(o.found, 'elements past the right edge').toEqual([])
    expect.soft(errors).toEqual([])
  })
}

test('phone menu', async ({ page }) => {
  await page.goto(`/${ORG}/overview`)
  await page.getByRole('button', { name: 'Open menu' }).click()
  const menu = page.getByRole('dialog', { name: 'Menu' })
  await expect(menu.getByRole('link', { name: 'Issues' })).toBeVisible()
  await shoot(page, 'phone-menu', { idle: false })
  await menu.getByRole('link', { name: 'Issues' }).click()
  await expect(page).toHaveURL(new RegExp(`/${ORG}/issues`))
  await expect(menu).toBeHidden()
})

test.describe('signed out', () => {
  test.use({ storageState: { cookies: [], origins: [] } })
  for (const [name, path] of [['login', '/login'], ['sign-up', '/registration/new'], ['password-new', '/passwords/new']]) {
    test(`phone ${name}`, async ({ page }) => {
      await page.goto(path)
      await shoot(page, `phone-${name}`, { fullPage: true })
      const o = await offscreen(page)
      console.log(name, JSON.stringify(o, null, 1))
      expect.soft(o.scroll).toBeLessThanOrEqual(1)
      expect.soft(o.found).toEqual([])
    })
  }
})
