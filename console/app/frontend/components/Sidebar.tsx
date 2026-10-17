import { Link, usePage } from '@inertiajs/react'
import { List, GitMerge, Settings, LogOut, ScrollText, Search, LayoutDashboard, BellRing } from 'lucide-react'
import { cn } from '@/lib/utils'
import { navSection } from '@/lib/nav'
import { SharedProps } from '@/types'
import { ModeToggle } from './ModeToggle'
import { usePaletteOpener } from './CommandPalette'
import { onMac } from './useShortcut'
import { keyLabel } from '@/lib/shortcuts'
import { buttonVariants } from '@/components/ui/button'

interface SidebarProps extends React.HTMLAttributes<HTMLDivElement> { }

export function Sidebar({ className }: SidebarProps) {
  const { url, props } = usePage<SharedProps>()
  const { current_org, current_user } = props
  const openPalette = usePaletteOpener()

  if (!current_org) return null

  const section = navSection(url)
  const navItems = [
    { label: 'Issues', href: `/${current_org.slug}/issues`, icon: List, active: section === 'issues' },
    { label: 'Logs', href: `/${current_org.slug}/logs`, icon: ScrollText, active: section === 'logs' },
    { label: 'Dashboards', href: `/${current_org.slug}/dashboards`, icon: LayoutDashboard, active: section === 'dashboards' },
    { label: 'Monitors', href: `/${current_org.slug}/monitors`, icon: BellRing, active: section === 'monitors' },
    { label: 'Projects', href: `/${current_org.slug}/projects`, icon: GitMerge, active: section === 'projects' },
    { label: 'Settings', href: `/${current_org.slug}/settings/organization`, icon: Settings, active: section === 'settings' },
  ]

  return (
    <div className={cn("min-h-screen border-r bg-card text-card-foreground md:flex hidden w-[240px] flex-shrink-0 flex-col justify-between z-20", className)}>
      <div className="flex flex-col flex-1">
        {/* Project Header */}
        <div className="h-14 flex items-center px-4 border-b">
          <div className="flex items-center gap-3">
            <div className="h-8 w-8 rounded bg-red-600 flex items-center justify-center text-white font-bold text-sm tracking-tight shadow-sm">
              {current_org?.slug?.charAt(0).toUpperCase()}
            </div>
            <div className="font-semibold text-sm tracking-tight">
              {current_org?.name}
            </div>
          </div>
        </div>

        {/* Navigation */}
        <div className="px-3 py-4 space-y-0.5">
          <button
            type="button"
            onClick={openPalette}
            className="mb-2 flex w-full items-center gap-3 rounded-md border px-3 py-1.5 text-[13px] text-muted-foreground hover:bg-muted/50 hover:text-foreground"
          >
            <Search className="h-4 w-4" />
            Search…
            <kbd className="ml-auto font-mono text-[11px]">{keyLabel(['Mod', 'k'], onMac())}</kbd>
          </button>
          {navItems.map((item) => (
            <Link
              key={item.href}
              href={item.href}
              className={cn(
                "flex items-center gap-3 rounded-md px-3 py-2 text-[14px] font-medium transition-colors",
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

      {/* User Footer */}
      <div className="p-3 border-t">
        <div className="flex items-center justify-between p-2 rounded-md hover:bg-muted/50 transition-colors cursor-pointer group mb-1">
          <div className="flex items-center gap-3 overflow-hidden">
            <div className="h-8 w-8 rounded-full bg-violet-500/10 flex items-center justify-center text-violet-600 border border-violet-200 text-xs font-bold">
              {current_user?.name?.charAt(0) || 'U'}
            </div>
            <div className="flex flex-col truncate">
              <span className="text-sm font-medium truncate">{current_user?.name}</span>
              <span className="text-xs text-muted-foreground truncate">{current_user?.email}</span>
            </div>
          </div>
        </div>

        <div className="flex items-center justify-between px-2">
          <span className="text-xs text-muted-foreground font-medium">Theme</span>
          <div className="flex items-center gap-1">
            <ModeToggle />
            <Link
              href="/session"
              method="delete"
              as="button"
              className={cn(buttonVariants({ variant: "ghost", size: "icon" }), "h-8 w-8 text-muted-foreground hover:text-foreground")}
              title="Sign out"
            >
              <LogOut className="h-4 w-4" />
            </Link>
          </div>
        </div>
      </div>
    </div>
  )
}
