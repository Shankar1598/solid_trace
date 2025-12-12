import { Link, usePage } from '@inertiajs/react'
import DashboardLayout from '@/components/layouts/DashboardLayout'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Issue, SharedProps } from '@/types'
import { formatDistanceToNow } from 'date-fns'
import { Checkbox } from '@/components/ui/checkbox'
import { Badge } from '@/components/ui/badge'
import { Sparkline } from '@/components/ui/sparkline'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Clock, AlertCircle, CheckCircle2 } from 'lucide-react'

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

  const getFilterUrl = (newFilters: Partial<typeof filters>) => {
    const params = new URLSearchParams({ ...filters, ...newFilters })
    if (params.get('status') === 'all') params.delete('status')
    if (params.get('environment') === 'all') params.delete('environment')
    if (!params.get('query')) params.delete('query')
    return `?${params.toString()}`
  }

  const currentTab = filters.status === 'resolved' ? 'resolved' : filters.status === 'unresolved' ? 'unresolved' : 'all'

  return (
    <DashboardLayout>
      <div className="space-y-4">
        <div className="flex items-center justify-between">
          <h1 className="text-2xl font-semibold tracking-tight">Issues</h1>
        </div>

        <div className="flex items-center space-x-2">
          <Input
            className="flex-1 bg-background"
            placeholder="Search for issues, stack traces, errors..."
            defaultValue={filters.query}
            onChange={(e) => {
              if (e.target.value === filters.query) return
              window.location.href = getFilterUrl({ query: e.target.value })
            }}
          />
          <Select
            defaultValue={filters.environment}
            onValueChange={(val) => window.location.href = getFilterUrl({ environment: val })}
          >
            <SelectTrigger className="w-[180px] bg-background">
              <SelectValue placeholder="Environment: All" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">Environment: All</SelectItem>
              {environments.map(env => (
                <SelectItem key={env} value={env}>{env}</SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        <div className="rounded-md border bg-card text-card-foreground shadow-sm">
          {/* Toolbar */}
          <div className="flex items-center justify-between border-b px-4 py-2 bg-muted/30">
            <div className="flex items-center space-x-4">
              <Checkbox id="select-all" />
              <Tabs
                value={currentTab}
                onValueChange={(val) => window.location.href = getFilterUrl({ status: val })}
                className="w-auto"
              >
                <TabsList className="h-8 bg-transparent p-0">
                  <TabsTrigger
                    value="unresolved"
                    className="data-[state=active]:bg-transparent data-[state=active]:shadow-none data-[state=active]:text-foreground data-[state=active]:font-semibold text-muted-foreground bg-transparent h-8 px-2 rounded-none border-b-2 border-transparent data-[state=active]:border-primary transition-none"
                  >
                    Unresolved
                  </TabsTrigger>
                  <TabsTrigger
                    value="resolved"
                    className="data-[state=active]:bg-transparent data-[state=active]:shadow-none data-[state=active]:text-foreground data-[state=active]:font-semibold text-muted-foreground bg-transparent h-8 px-2 rounded-none border-b-2 border-transparent data-[state=active]:border-primary transition-none"
                  >
                    Resolved
                  </TabsTrigger>
                  <TabsTrigger
                    value="all"
                    className="data-[state=active]:bg-transparent data-[state=active]:shadow-none data-[state=active]:text-foreground data-[state=active]:font-semibold text-muted-foreground bg-transparent h-8 px-2 rounded-none border-b-2 border-transparent data-[state=active]:border-primary transition-none"
                  >
                    All Issues
                  </TabsTrigger>
                </TabsList>
              </Tabs>
            </div>

            <div className="flex items-center text-xs text-muted-foreground font-medium space-x-6 pr-4">
              <div className="w-24 text-right">Graph</div>
              <div className="w-16 text-right">Events</div>
              <div className="w-16 text-right">Users</div>
            </div>
          </div>

          {/* List */}
          <div className="divide-y">
            {issues.length === 0 ? (
              <div className="flex flex-col items-center justify-center py-20 text-center text-muted-foreground">
                <p>No issues found matching your criteria.</p>
              </div>
            ) : (
              issues.map((issue) => (
                <div key={issue.id} className="group flex items-center hover:bg-muted/40 transition-colors">
                  <div className="flex items-center pl-4 pr-3 py-3">
                    <Checkbox />
                  </div>

                  <Link
                    href={`/${current_org?.slug}/${issue.project.slug}/issues/${issue.number}`}
                    className="flex-1 min-w-0 py-3 pr-4 flex items-start"
                  >
                    <div className="flex-1 min-w-0 mr-6">
                      <div className="flex items-center gap-2 mb-1">
                        <h3 className="font-semibold text-base truncate text-foreground group-hover:text-primary transition-colors">
                          {issue.title}
                        </h3>
                        <Badge variant="outline" className="font-mono text-[10px] h-5 px-1.5 font-normal text-muted-foreground uppercase tracking-wider">
                          {issue.kind}
                        </Badge>
                      </div>
                      <div className="text-sm text-muted-foreground truncate font-mono">
                        {issue.culprit}
                      </div>
                      <div className="flex items-center gap-3 mt-1.5 text-xs text-muted-foreground">
                        <div className="flex items-center gap-1">
                          {issue.status === 'resolved' ? (
                            <CheckCircle2 className="h-3 w-3 text-green-500" />
                          ) : (
                            <AlertCircle className="h-3 w-3 text-red-500" />
                          )}
                          <span>{issue.project.name}</span>
                        </div>
                        <span>•</span>
                        <div className="flex items-center gap-1" title={issue.created_at}>
                          <Clock className="h-3 w-3" />
                          {formatDistanceToNow(new Date(issue.created_at), { addSuffix: true })}
                        </div>
                      </div>
                    </div>

                    <div className="flex items-center space-x-6 text-sm text-muted-foreground">
                      <div className="w-24 flex justify-end">
                        <Sparkline />
                      </div>
                      <div className="w-16 text-right font-medium text-foreground">
                        {new Intl.NumberFormat('en-US', { notation: "compact" }).format(issue.events_count)}
                      </div>
                      <div className="w-16 text-right font-medium text-foreground flex justify-end">
                        {new Intl.NumberFormat('en-US', { notation: "compact" }).format(0)}
                      </div>
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
