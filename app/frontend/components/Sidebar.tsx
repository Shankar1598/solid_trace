import { Link, usePage } from '@inertiajs/react'
import { Layout, GitMerge, Settings, Users, LogOut } from 'lucide-react'
import { cn } from '@/lib/utils'
import { SharedProps } from '@/types'
import { ModeToggle } from './ModeToggle'
import { Button } from '@/components/ui/button'

interface SidebarProps extends React.HTMLAttributes<HTMLDivElement> { }

export function Sidebar({ className }: SidebarProps) {
  const { url, props } = usePage<SharedProps>()
  const { current_org, current_user } = props

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
    <div className={cn("pb-4 min-h-screen border-r bg-zinc-900 text-zinc-300 md:flex hidden w-64 flex-shrink-0 flex-col justify-between", className)}>
      <div className="space-y-4 py-4 flex-1">
        <div className="px-3 py-2">
          <div className="mb-6 px-4 flex items-center gap-2 font-semibold text-white">
            <div className="h-6 w-6 rounded-full bg-purple-500" />
            <span>Garnet</span>
          </div>

          <div className="space-y-1">
            <h2 className="mb-2 px-4 text-xs font-semibold tracking-tight text-zinc-500 uppercase">
              {current_org.name}
            </h2>
            {navItems.map((item) => (
              <Link
                key={item.href}
                href={item.href}
                className={cn(
                  "flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium transition-colors",
                  item.active
                    ? "bg-white/10 text-white"
                    : "hover:bg-white/5 hover:text-white text-zinc-400"
                )}
              >
                <item.icon className="h-4 w-4" />
                {item.label}
              </Link>
            ))}
          </div>
        </div>

        <div className="px-3 py-2">
          <h2 className="mb-2 px-4 text-xs font-semibold tracking-tight text-zinc-500 uppercase">
            Settings
          </h2>
          <div className="space-y-1">
            {settingsItems.map((item) => (
              <Link
                key={item.href}
                href={item.href}
                className={cn(
                  "flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium transition-colors",
                  item.active
                    ? "bg-white/10 text-white"
                    : "hover:bg-white/5 hover:text-white text-zinc-400"
                )}
              >
                <item.icon className="h-4 w-4" />
                {item.label}
              </Link>
            ))}
          </div>
        </div>
      </div>

      <div className="flex border-t border-zinc-800">
        <div className="flex items-center justify-between px-2">
          <div className="flex items-center gap-2 overflow-hidden">
            <div className="h-8 w-8 rounded-full bg-indigo-500 flex items-center justify-center text-white text-xs font-bold">
              {current_user?.name?.charAt(0) || current_user?.email?.charAt(0).toUpperCase()}
            </div>
            <div className="flex flex-col truncate">
              <span className="text-sm font-medium text-white truncate max-w-[100px]">{current_user?.name}</span>
              <span className="text-xs text-zinc-500 truncate max-w-[100px]">{current_user?.email}</span>
            </div>
          </div>
        </div>
        <div className="mt-4 flex items-center justify-between px-2">
          <ModeToggle />
          <Link href="/session" method="delete" as="button">
            <Button variant="ghost" size="icon" className="text-zinc-400 hover:text-white hover:bg-white/5">
              <LogOut className="h-4 w-4" />
            </Button>
          </Link>
        </div>
      </div>
    </div>
  )
}
