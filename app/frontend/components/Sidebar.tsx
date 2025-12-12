import { Link, usePage } from '@inertiajs/react'
import { Layout, GitMerge, Settings, Users } from 'lucide-react'
import { cn } from '@/lib/utils'
import { SharedProps } from '@/types'

interface SidebarProps extends React.HTMLAttributes<HTMLDivElement> { }

export function Sidebar({ className }: SidebarProps) {
  const { url, props } = usePage<SharedProps>()
  const { current_org } = props

  if (!current_org) return null

  const navItems = [
    {
      label: 'Issues',
      href: `/${current_org.slug}/issues`, // We'll fix the routing dynamically later
      icon: Layout,
      active: url.startsWith(`/${current_org.slug}/issues`)
    },
    {
      label: 'Projects',
      href: `/${current_org.slug}/projects`,
      icon: GitMerge,
      active: url.startsWith(`/${current_org.slug}/projects`)
    },
    // Mentions would go here
  ]

  const settingsItems = [
    {
      label: 'Members',
      href: `/${current_org.slug}/settings/members`,
      icon: Users,
      active: url.includes('/settings/members')
    },
    {
      label: 'Settings',
      href: `/${current_org.slug}/settings`,
      icon: Settings,
      active: url.endsWith('/settings')
    }
  ]

  return (
    <div className={cn("pb-12 min-h-screen border-r bg-background", className)}>
      <div className="space-y-4 py-4">
        <div className="px-3 py-2">
          <div className="mb-6 px-4 flex items-center gap-2 font-semibold">
            <div className="h-6 w-6 rounded-full bg-primary" />
            <span>Garnet</span>
          </div>

          <div className="space-y-1">
            <h2 className="mb-2 px-4 text-xs font-semibold tracking-tight text-muted-foreground">
              {current_org.name}
            </h2>
            {navItems.map((item) => (
              <Link
                key={item.href}
                href={item.href}
                className={cn(
                  "flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium hover:bg-accent hover:text-accent-foreground transition-colors",
                  item.active ? "bg-accent text-accent-foreground" : "text-transparent-foreground"
                )}
              >
                <item.icon className="h-4 w-4" />
                {item.label}
              </Link>
            ))}
          </div>
        </div>

        <div className="px-3 py-2">
          <h2 className="mb-2 px-4 text-lg font-semibold tracking-tight">
            Settings
          </h2>
          <div className="space-y-1">
            {settingsItems.map((item) => (
              <Link
                key={item.href}
                href={item.href}
                className={cn(
                  "flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium hover:bg-accent hover:text-accent-foreground transition-colors",
                  item.active ? "bg-accent text-accent-foreground" : "text-transparent-foreground"
                )}
              >
                <item.icon className="h-4 w-4" />
                {item.label}
              </Link>
            ))}
          </div>
        </div>
      </div>
    </div>
  )
}
