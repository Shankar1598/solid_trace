import { Fragment } from 'react'
import { Link, usePage } from '@inertiajs/react'
import { Gauge, List, GitMerge, Settings, LogOut, ScrollText, Search, LayoutDashboard, BellRing, Check, ChevronsUpDown, Timer } from 'lucide-react'
import { cn } from '@/lib/utils'
import { navSection, orgSwitchUrl, performanceProject } from '@/lib/nav'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { SharedProps } from '@/types'
import { ModeToggle } from './ModeToggle'
import { usePaletteOpener } from './CommandPalette'
import { onMac } from './useShortcut'
import { keyLabel } from '@/lib/shortcuts'
import { buttonVariants } from '@/components/ui/button'

interface SidebarProps extends React.HTMLAttributes<HTMLDivElement> { }

/** Performance, which belongs to a Project: a heading, with each Project's under it. */
function PerformanceNav({ org, projects, current }: { org: string; projects: { name: string; slug: string }[]; current: string | null }) {
  return (
    <div role="group" aria-label="Performance">
      <div className={cn("flex items-center gap-3 px-3 py-2 text-sm font-medium", current ? "text-primary" : "text-muted-foreground")}>
        <Timer className="h-4 w-4" />
        Performance
      </div>
      {projects.map(project => (
        <Link
          key={project.slug}
          href={`/${org}/projects/${project.slug}/performance`}
          aria-current={current === project.slug ? 'page' : undefined}
          className={cn(
            "block truncate py-1 pr-3 pl-10 text-[13px] transition-colors",
            current === project.slug ? "bg-secondary text-primary" : "text-muted-foreground hover:bg-muted/50 hover:text-foreground"
          )}
        >
          {project.name}
        </Link>
      ))}
    </div>
  )
}

export function Sidebar({ className }: SidebarProps) {
  const { url, props } = usePage<SharedProps>()
  const { current_org, current_user } = props
  const openPalette = usePaletteOpener()

  if (!current_org) return null

  const organizations = props.organizations ?? []
  const projects = props.org_projects ?? []
  const orgHeader = (
    <>
      <div className="h-8 w-8 shrink-0 bg-foreground flex items-center justify-center text-background font-mono font-semibold text-sm">
        {current_org.slug.charAt(0).toUpperCase()}
      </div>
      <div className="min-w-0 truncate font-semibold text-sm tracking-tight">
        {current_org.name}
      </div>
    </>
  )

  const section = navSection(url)
  const navItems = [
    { label: 'Overview', href: `/${current_org.slug}/overview`, icon: Gauge, active: section === 'overview' },
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
        {/* Organization Header: a selector with two or more Organizations */}
        <div className="h-14 flex items-center px-4 border-b">
          {organizations.length > 1 ? (
            <DropdownMenu>
              <DropdownMenuTrigger
                aria-label="Switch Organization"
                className="-mx-2 flex min-w-0 flex-1 items-center gap-3 px-2 py-1 text-left hover:bg-muted/50 data-popup-open:bg-muted/50"
              >
                {orgHeader}
                <ChevronsUpDown className="ml-auto h-4 w-4 shrink-0 text-muted-foreground" />
              </DropdownMenuTrigger>
              <DropdownMenuContent className="min-w-52">
                {organizations.map(org => {
                  const current = org.slug === current_org.slug
                  return (
                    <DropdownMenuItem
                      key={org.slug}
                      aria-current={current ? 'page' : undefined}
                      // The current Organization only closes the menu.
                      render={current ? undefined : <Link href={orgSwitchUrl(url, org.slug)} />}
                    >
                      <span className="min-w-0 flex-1 truncate">{org.name}</span>
                      {current && <Check className="h-3.5 w-3.5" aria-label="Current Organization" />}
                    </DropdownMenuItem>
                  )
                })}
              </DropdownMenuContent>
            </DropdownMenu>
          ) : (
            <div className="flex items-center gap-3">{orgHeader}</div>
          )}
        </div>

        <nav aria-label="Main" className="px-3 py-4 space-y-0.5">
          <button
            type="button"
            onClick={openPalette}
            className="mb-2 flex w-full items-center gap-3 border px-3 py-1.5 text-[13px] text-muted-foreground hover:bg-muted/50 hover:text-foreground"
          >
            <Search className="h-4 w-4" />
            Search…
            <kbd className="ml-auto font-mono text-[11px]">{keyLabel(['Mod', 'k'], onMac())}</kbd>
          </button>
          {navItems.map((item) => (
            <Fragment key={item.href}>
              <Link
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
              {item.label === 'Logs' && projects.length > 0 && (
                <PerformanceNav org={current_org.slug} projects={projects} current={performanceProject(url)} />
              )}
            </Fragment>
          ))}
        </nav>
      </div>

      {/* User Footer */}
      <div className="p-3 border-t">
        <div className="flex items-center justify-between p-2 hover:bg-muted/50 transition-colors cursor-pointer group mb-1">
          <div className="flex items-center gap-3 overflow-hidden">
            <div className="h-8 w-8 rounded-full bg-muted flex items-center justify-center text-foreground border text-xs font-semibold">
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
