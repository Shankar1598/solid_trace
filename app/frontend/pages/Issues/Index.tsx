import { Link, usePage } from '@inertiajs/react'
import DashboardLayout from '@/components/layouts/DashboardLayout'
import { Card, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Issue, SharedProps } from '@/types'
import { cn } from '@/lib/utils'
import { formatDistanceToNow } from 'date-fns'

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

  // Simple helper to construct filter URL
  const getFilterUrl = (newFilters: Partial<typeof filters>) => {
    const params = new URLSearchParams({ ...filters, ...newFilters })
    // Remove "all" or empty values to keep URL clean
    if (params.get('status') === 'all') params.delete('status')
    if (params.get('environment') === 'all') params.delete('environment')
    if (!params.get('query')) params.delete('query')

    return `?${params.toString()}`
  }

  return (
    <DashboardLayout>
      <div className="space-y-6">
        <div className="flex items-center justify-between">
          <h1 className="text-3xl font-bold tracking-tight">Issues</h1>
        </div>

        <div className="flex items-center gap-4">
          <div className="flex-1">
            <Input
              placeholder="Search issues..."
              defaultValue={filters.query}
              onChange={(e) => {
                // In a real app, you'd debounce this and use router.visit
                if (e.target.value === filters.query) return
                window.location.href = getFilterUrl({ query: e.target.value })
              }}
            />
          </div>
          <Select
            defaultValue={filters.status}
            onValueChange={(val) => window.location.href = getFilterUrl({ status: val })}
          >
            <SelectTrigger className="w-[180px]">
              <SelectValue placeholder="Status" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">All Statuses</SelectItem>
              <SelectItem value="unresolved">Unresolved</SelectItem>
              <SelectItem value="resolved">Resolved</SelectItem>
            </SelectContent>
          </Select>

          <Select
            defaultValue={filters.environment}
            onValueChange={(val) => window.location.href = getFilterUrl({ environment: val })}
          >
            <SelectTrigger className="w-[180px]">
              <SelectValue placeholder="Environment" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">All Environments</SelectItem>
              {environments.map(env => (
                <SelectItem key={env} value={env}>{env}</SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        <div className="grid gap-4">
          {issues.length === 0 ? (
            <Card>
              <CardContent className="flex flex-col items-center justify-center py-10 text-center text-muted-foreground">
                <p>No issues found matching your criteria.</p>
              </CardContent>
            </Card>
          ) : (
            issues.map((issue) => (
              <Link
                key={issue.id}
                href={`/${current_org?.slug}/issues/${issue.number}`}
                className="block"
              >
                <Card className="hover:bg-accent/50 transition-colors">
                  <CardContent className="p-4 flex items-start gap-4">
                    <div className={cn(
                      "mt-1 h-3 w-3 rounded-full flex-shrink-0",
                      issue.status === 'resolved' ? "bg-green-500" : "bg-red-500"
                    )} />
                    <div className="flex-1 min-w-0 space-y-1">
                      <div className="flex items-center justify-between">
                        <p className="font-medium text-lg leading-none truncate">
                          {issue.title}
                        </p>
                        <span className="text-xs text-muted-foreground whitespace-nowrap ml-4">
                          {formatDistanceToNow(new Date(issue.created_at), { addSuffix: true })}
                        </span>
                      </div>
                      <p className="text-sm text-muted-foreground truncate">
                        {issue.culprit}
                      </p>
                      <div className="flex items-center gap-2 text-xs text-muted-foreground">
                        <span className="font-mono bg-muted px-1.5 py-0.5 rounded">
                          {issue.project.name}
                        </span>
                        <span>•</span>
                        <span>#{issue.number}</span>
                        <span>•</span>
                        <span>{issue.events_count} events</span>
                      </div>
                    </div>
                  </CardContent>
                </Card>
              </Link>
            ))
          )}
        </div>
      </div>
    </DashboardLayout>
  )
}
