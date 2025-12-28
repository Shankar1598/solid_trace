import { Link, usePage } from '@inertiajs/react'
import { Building2, Users, Puzzle, UserCircle } from 'lucide-react'
import { cn } from '@/lib/utils'
import { SharedProps } from '@/types'

export function SettingsSidebar() {
  const { url, props } = usePage<SharedProps>()
  const { current_org } = props

  if (!current_org) return null

  const settingsItems = [
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
          href: `/${current_org.slug}/settings/members`,
          icon: Users,
          active: url.includes('/settings/members')
        },
        {
          label: 'Integrations',
          href: `/${current_org.slug}/settings/integrations`,
          icon: Puzzle,
          active: url.includes('/settings/integrations')
        }
      ]
    },
    {
      group: 'User',
      items: [
        {
          label: 'Account',
          href: `/${current_org.slug}/settings/user`,
          icon: UserCircle,
          active: url.includes('/settings/user')
        }
      ]
    }
  ]

  return (
    <div className="w-64 flex-shrink-0 border-r bg-card h-full hidden md:block">
      <div className="flex flex-col h-full">
        {/* Header */}
        <div className="h-14 flex items-center px-6 border-b">
          <span className="text-sm font-semibold tracking-tight uppercase text-muted-foreground/70">Settings</span>
        </div>

        {/* Navigation */}
        <div className="flex-1 py-6 px-3 space-y-6 overflow-y-auto">
          {settingsItems.map((group) => (
            <div key={group.group} className="space-y-2">
              <h3 className="px-3 text-[10px] font-bold text-muted-foreground/50 uppercase tracking-[0.2em]">
                {group.group}
              </h3>
              <div className="space-y-0.5">
                {group.items.map((item) => (
                  <Link
                    key={item.href}
                    href={item.href}
                    className={cn(
                      "flex items-center gap-3 px-3 py-2 text-[13px] font-medium transition-colors",
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
