import { Link, usePage, router } from '@inertiajs/react'
import { useState, useEffect } from 'react'
import DashboardLayout from '@/components/layouts/DashboardLayout'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Issue, Project, SharedProps } from '@/types'
import { formatDistanceToNow } from 'date-fns'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"


const TIME_RANGE_OPTIONS = { '24h': '24H', '14d': '14D', '30d': '30D' }
const SORT_OPTIONS = {
  last_seen: 'Last Seen',
  created: 'First Seen',
  priority: 'Priority'
}

interface IssuesIndexProps {
  issues: Issue[]
  projects: Project[]
  filters: {
    status: string
    query: string
    environment: string
    project_id: string
  }
}

export default function IssuesIndex({ issues, projects, filters }: IssuesIndexProps) {
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
    if (params.get('project_id') === 'all') params.delete('project_id')
    if (!params.get('query')) params.delete('query')
    return `?${params.toString()}`
  }

  const projectOptions: Record<string, string> = {
    all: 'All Projects',
    ...Object.fromEntries(projects.map(p => [p.id.toString(), p.name]))
  }

  return (
    <DashboardLayout>
      <div className="flex flex-col h-full space-y-4">
        <div className="flex items-center justify-between px-1">
          <h1 className="text-2xl font-bold tracking-tight">Issues</h1>
        </div>

        <div className="flex items-center gap-2 w-full">
          <div className="flex items-center border">
            <Select
              value={filters.project_id}
              onValueChange={(val) => router.get(getFilterUrl({ project_id: val || 'all' }), {}, { preserveState: true, replace: true, preserveScroll: true })}
              items={projectOptions}
            >
              <SelectTrigger className="border-0 border-r">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {Object.entries(projectOptions).map(([value, label]) => (
                  <SelectItem key={value} value={value}>{label}</SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Select defaultValue="14d" items={TIME_RANGE_OPTIONS}>
              <SelectTrigger className="border-0">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {Object.entries(TIME_RANGE_OPTIONS).map(([value, label]) => (
                  <SelectItem key={value} value={value}>{label}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div className="flex-1 relative">
            <Input
              className="pl-2 text-sm"
              placeholder="Search for issues..."
              value={searchTerm}
              onChange={(e) => setSearchTerm(e.target.value)}
            />
          </div>

          <div className="flex items-center gap-2 shrink-0">
            <Select defaultValue="last_seen" items={SORT_OPTIONS}>
              <SelectTrigger>
                <span className="text-muted-foreground mr-1">Sort:</span>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {Object.entries(SORT_OPTIONS).map(([value, label]) => (
                  <SelectItem key={value} value={value}>{label}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        </div>

        <Table className="border shadow-sm table-fixed">
          <TableHeader className="bg-muted/30">
            <TableRow className="hover:bg-transparent border-b">
              <TableHead className="w-[48px] px-4">
                <div className="flex items-center justify-center">
                  <Checkbox id="select-all" />
                </div>
              </TableHead>
              <TableHead className="text-xs font-semibold text-muted-foreground uppercase tracking-wider">Issue</TableHead>
              <TableHead className="text-right text-xs font-semibold text-muted-foreground uppercase tracking-wider px-4 w-32">Last Seen</TableHead>
              <TableHead className="text-right text-xs font-semibold text-muted-foreground uppercase tracking-wider px-4 w-32">Age</TableHead>
              <TableHead className="text-right text-xs font-semibold text-muted-foreground uppercase tracking-wider px-4 w-24">Events</TableHead>
              <TableHead className="text-right text-xs font-semibold text-muted-foreground uppercase tracking-wider px-4 w-24">Users</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {issues.length === 0 ? (
              <TableRow>
                <TableCell colSpan={6} className="h-48 text-center text-muted-foreground">
                  No issues found matching your criteria.
                </TableCell>
              </TableRow>
            ) : (
              issues.map((issue) => (
                <TableRow key={issue.id} className="group cursor-pointer hover:bg-muted/40 transition-colors">
                  <TableCell className="px-4 py-3" onClick={(e) => e.stopPropagation()}>
                    <div className="flex items-center justify-center">
                      <Checkbox />
                    </div>
                  </TableCell>
                  <TableCell className="p-0">
                    <Link
                      href={`/${current_org?.slug}/projects/${issue.project.slug}/issues/${issue.number}`}
                      className="block px-2 py-3"
                    >
                      <div className="flex flex-col min-w-0">
                        <div className="flex items-baseline gap-2 mb-1">
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
                    </Link>
                  </TableCell>
                  <TableCell className="text-right px-4 py-3 text-sm text-foreground/80 font-mono">
                    {formatDistanceToNow(new Date(issue.last_seen_at || issue.created_at), { addSuffix: false }).replace('about ', '')}
                  </TableCell>
                  <TableCell className="text-right px-4 py-3 text-sm text-muted-foreground font-mono">
                    {formatDistanceToNow(new Date(issue.created_at), { addSuffix: false }).replace('about ', '')}
                  </TableCell>
                  <TableCell className="text-right px-4 py-3 text-sm font-medium text-foreground font-mono">
                    {new Intl.NumberFormat('en-US', { notation: "compact" }).format(issue.events_count)}
                  </TableCell>
                  <TableCell className="text-right px-4 py-3 text-sm font-medium text-foreground font-mono">
                    {new Intl.NumberFormat('en-US', { notation: "compact" }).format(0)}
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </div>
    </DashboardLayout>
  )
}
