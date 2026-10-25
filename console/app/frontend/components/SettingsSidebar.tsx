import { useEffect, useRef } from 'react'
import { Link, usePage } from '@inertiajs/react'
import { Building2, Users, Puzzle, UserCircle, KeyRound, Mail, Server } from 'lucide-react'
import { cn } from '@/lib/utils'
import { SharedProps } from '@/types'

function useSettingsItems() {
  const { url, props } = usePage<SharedProps>()
  const { current_org, current_user } = props

  if (!current_org) return null

  return [
    {
      group: 'Organization',
      items: [
        {
          label: 'General',
          href: `/${current_org.slug}/settings/organization`,
          icon: Building2,
          active: url === `/${current_org.slug}/settings/organization`
        },
        {
          label: 'Members',
          href: `/${current_org.slug}/settings/organization_users`,
          icon: Users,
          active: url.includes('/settings/organization_users')
        },
        {
          label: 'Integrations',
          href: `/${current_org.slug}/settings/integrations`,
          icon: Puzzle,
          active: url.includes('/settings/integrations')
        }
      ]
    },
    // The install's settings, which apply to every Organization: only an instance admin sees them.
    ...(current_user?.instance_admin ? [{
      group: 'Install',
      items: [
        {
          label: 'Email',
          href: `/${current_org.slug}/settings/email`,
          icon: Mail,
          active: url.startsWith(`/${current_org.slug}/settings/email`)
        },
        {
          label: 'Instance',
          href: `/${current_org.slug}/settings/instance`,
          icon: Server,
          active: url.startsWith(`/${current_org.slug}/settings/instance`)
        }
      ]
    }] : []),
    {
      group: 'User',
      items: [
        {
          label: 'Account',
          href: `/${current_org.slug}/settings/user`,
          icon: UserCircle,
          active: url.startsWith(`/${current_org.slug}/settings/user`)
        },
        {
          label: 'API tokens & MCP',
          href: `/${current_org.slug}/settings/api_tokens`,
          icon: KeyRound,
          active: url.startsWith(`/${current_org.slug}/settings/api_tokens`)
        }
      ]
    }
  ]
}

/** Below md, where the sidebar is hidden: the same pages as a row of tabs that scrolls sideways. */
export function SettingsTabs() {
  const settingsItems = useSettingsItems()
  const row = useRef<HTMLElement>(null)
  // The current page's tab may sit past the edge: bring it into the row.
  useEffect(() => {
    row.current?.querySelector('[aria-current="page"]')?.scrollIntoView({ block: 'nearest', inline: 'nearest' })
  })
  if (!settingsItems) return null

  return (
    <nav ref={row} aria-label="Settings" className="md:hidden flex gap-1 overflow-x-auto border-b px-2 py-2">
      {settingsItems.flatMap(group => group.items).map(item => (
        <Link
          key={item.href}
          href={item.href}
          aria-current={item.active ? 'page' : undefined}
          className={cn(
            "flex shrink-0 items-center gap-2 px-3 py-1.5 text-sm font-medium whitespace-nowrap transition-colors",
            item.active
              ? "bg-secondary text-primary"
              : "text-muted-foreground hover:bg-muted/50 hover:text-foreground"
          )}
        >
          <item.icon className="h-4 w-4" />
          {item.label}
        </Link>
      ))}
    </nav>
  )
}

export function SettingsSidebar() {
  const settingsItems = useSettingsItems()
  if (!settingsItems) return null

  return (
    <div className="w-64 flex-shrink-0 border-r bg-card h-full hidden md:block">
      <div className="flex flex-col h-full">
        {/* Header */}
        <div className="h-14 flex items-center px-6 border-b">
          <span className="text-sm font-semibold">Settings</span>
        </div>

        {/* Navigation */}
        <div className="flex-1 py-6 px-3 space-y-6 overflow-y-auto">
          {settingsItems.map((group) => (
            <div key={group.group} className="space-y-2">
              <h3 className="caps-label px-3">
                {group.group}
              </h3>
              <div className="space-y-0.5">
                {group.items.map((item) => (
                  <Link
                    key={item.href}
                    href={item.href}
                    className={cn(
                      "flex items-center gap-3 px-3 py-2 text-sm font-medium transition-colors",
                      item.active
                        ? "bg-secondary text-primary"
                        : "text-muted-foreground hover:bg-muted/50 hover:text-foreground"
                    )}
                  >
                    <item.icon className="h-4 w-4" />
                    {item.label}
                  </Link>
                ))}
              </div>
            </div>
          ))}
        </div>
      </div>
    </div>
  )
}
