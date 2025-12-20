import { Link, usePage, router } from '@inertiajs/react'
import { useState, useEffect } from 'react'
import DashboardLayout from '@/components/layouts/DashboardLayout'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Issue, SharedProps } from '@/types'
import { formatDistanceToNow } from 'date-fns'
import { Checkbox } from '@/components/ui/checkbox'
import { Sparkline } from '@/components/ui/sparkline'

interface IssuesIndexProps {
  issues: Issue[]
  environments: string[]
  filters: {
    status: string
    query: string
    environment: string
  }
}

export default function IssuesIndex({ issues, environments, filters }: IssuesIndexProps) {
  const { current_org } = usePage<SharedProps>().props
  const [searchTerm, setSearchTerm] = useState(filters.query || '')

  useEffect(() => {
    const handler = setTimeout(() => {
      // Only trigger if the search term has actually changed from what's currently filtered
      if (searchTerm !== (filters.query || '')) {
        router.get(
          getFilterUrl({ query: searchTerm }),
          {},
          { preserveState: true, replace: true, preserveScroll: true }
        )
      }
    }, 300)

    return () => clearTimeout(handler)
  }, [searchTerm])

  const getFilterUrl = (newFilters: Partial<typeof filters>) => {
    const params = new URLSearchParams({ ...filters, ...newFilters })
    if (params.get('status') === 'all') params.delete('status')
    if (params.get('environment') === 'all') params.delete('environment')
    if (!params.get('query')) params.delete('query')
    return `?${params.toString()}`
  }



  return (
    <DashboardLayout>
      <div className="flex flex-col h-full space-y-4">
        <div className="flex items-center justify-between px-1">
          <h1 className="text-xl font-bold tracking-tight">Issues</h1>
        </div>

        {/* Filter Bar */}
        <div className="flex items-center gap-2 w-full">
          <div className="flex items-center rounded-md border bg-background shadow-sm h-9">
            <Select defaultValue="all">
              <SelectTrigger className="w-[130px] border-0 focus:ring-0 h-8 rounded-r-none border-r">
                <SelectValue placeholder="All Projects" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">All Projects</SelectItem>
                {/* Dynamically populate projects if available, for now static */}
              </SelectContent>
            </Select>
            <Select
              defaultValue={filters.environment}
              onValueChange={(val) => window.location.href = getFilterUrl({ environment: val })}
            >
              <SelectTrigger className="w-[110px] border-0 focus:ring-0 h-8 rounded-none border-r">
                <SelectValue placeholder="All Envs" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">All Envs</SelectItem>
                {environments.map(env => (
                  <SelectItem key={env} value={env}>{env}</SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Select defaultValue="14d">
              <SelectTrigger className="w-[80px] border-0 focus:ring-0 h-8 rounded-l-none">
                <SelectValue placeholder="14D" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="24h">24H</SelectItem>
                <SelectItem value="14d">14D</SelectItem>
              </SelectContent>
            </Select>
          </div>

          <div className="flex-1 relative">
            <Input
              className="pl-2 bg-background h-9 font-mono text-sm"
              placeholder="Search for issues..."
              value={searchTerm}
              onChange={(e) => setSearchTerm(e.target.value)}
            />
          </div>

          <div className="flex items-center gap-2 shrink-0">
            <Select defaultValue="last_seen">
              <SelectTrigger className="bg-background h-9">
                <span className="text-muted-foreground mr-1">Sort:</span>
                <SelectValue placeholder="Last Seen" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="last_seen">Last Seen</SelectItem>
                <SelectItem value="created">First Seen</SelectItem>
                <SelectItem value="priority">Priority</SelectItem>
              </SelectContent>
            </Select>
            <button className="bg-primary/90 text-primary-foreground hover:bg-primary px-3 py-1.5 rounded-md text-sm font-medium h-9 shadow-sm shrink-0">
              Save As
            </button>
          </div>
        </div>


        <div className="rounded-md border bg-card text-card-foreground shadow-sm overflow-hidden">
          {/* List Header */}
          <div className="grid grid-cols-12 gap-4 px-4 py-2 bg-muted/30 border-b text-xs font-semibold text-muted-foreground uppercase tracking-wider items-center">
            <div className="col-span-6 flex items-center gap-3">
              <Checkbox id="select-all" className="translate-y-[1px]" />
              <span>Issue</span>
            </div>
            <div className="col-span-1 text-right">Last Seen</div>
            <div className="col-span-1 text-right">Age</div>
            <div className="col-span-2 text-right">Trend</div>
            <div className="col-span-1 text-right">Events</div>
            <div className="col-span-1 text-right">Users</div>
          </div>

          {/* List Items */}
          <div className="divide-y">
            {issues.length === 0 ? (
              <div className="flex flex-col items-center justify-center py-20 text-center text-muted-foreground">
                <p>No issues found matching your criteria.</p>
              </div>
            ) : (
              issues.map((issue) => (
                <div key={issue.id} className="group relative hover:bg-muted/40 transition-colors">
                  {/* Status Indicator Bar Removed */}

                  <Link
                    href={`/${current_org?.slug}/projects/${issue.project.slug}/issues/${issue.number}`}
                    className="grid grid-cols-12 gap-4 px-4 py-3 items-center"
                  >
                    {/* Issue Info */}
                    <div className="col-span-6 flex items-start gap-3 min-w-0">
                      <div className="flex items-center h-full pt-1" onClick={(e) => e.stopPropagation()}>
                        <Checkbox />
                      </div>
                      <div className="flex-1 min-w-0">
                        <div className="flex items-baseline gap-2 mb-0.5">
                          <h3 className="font-semibold text-[15px] truncate text-foreground group-hover:text-primary transition-colors">
                            {issue.title}
                          </h3>
                          <span className="text-[13px] text-muted-foreground truncate font-normal">
                            {issue.culprit}
                          </span>
                        </div>
                        <div className="flex items-center gap-2 text-xs text-muted-foreground">
                          <code className="px-1 py-0.5 rounded bg-muted font-mono text-[10px] text-foreground border">
                            {issue.kind || 'error'}
                          </code>
                          {issue.status === 'unresolved' && (
                            <span className="text-red-500 font-medium text-[10px] uppercase">Unhandled</span>
                          )}
                          <span className="text-muted-foreground/60">{issue.project.name}</span>
                        </div>
                      </div>
                    </div>

                    <div className="col-span-1 text-right text-sm text-foreground/80 font-mono">
                      {/* Last Seen - Mocked or Real */}
                      {formatDistanceToNow(new Date(issue.last_seen_at || issue.created_at), { addSuffix: false }).replace('about ', '')}
                    </div>
                    <div className="col-span-1 text-right text-sm text-muted-foreground font-mono">
                      {/* Age */}
                      {formatDistanceToNow(new Date(issue.created_at), { addSuffix: false }).replace('about ', '')}
                    </div>
                    <div className="col-span-2 flex justify-end pr-2 h-8">
                      <Sparkline />
                    </div>
                    <div className="col-span-1 text-right text-sm font-medium text-foreground font-mono">
                      {new Intl.NumberFormat('en-US', { notation: "compact" }).format(issue.events_count)}
                    </div>
                    <div className="col-span-1 text-right text-sm font-medium text-foreground font-mono">
                      {new Intl.NumberFormat('en-US', { notation: "compact" }).format(0)}
                    </div>
                  </Link>
                </div>
              ))
            )}
          </div>
        </div>
      </div>
    </DashboardLayout>
  )
}
