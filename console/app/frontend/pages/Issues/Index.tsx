import { Link, usePage, router } from '@inertiajs/react'
import { useState, useEffect } from 'react'
import DashboardLayout from '@/components/layouts/DashboardLayout'
import { PageHeader } from '@/components/PageHeader'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Issue, Project, SharedProps } from '@/types'
import { formatDistanceToNow } from 'date-fns'
import { Checkbox } from '@/components/ui/checkbox'
import { Button } from '@/components/ui/button'
import { ArrowLeft, ArrowRight } from 'lucide-react'
import { issuesFilterQuery, TIME_RANGE_OPTIONS, type IssueFilters } from '@/lib/issues/filters'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"


const SORT_OPTIONS = {
  last_seen: 'Last seen',
  first_seen: 'First seen',
}

interface IssuesIndexProps {
  issues: Issue[]
  pagination: {
    current_page: number
    total_pages: number
    total_count: number
  }
  projects: Project[]
  filters: IssueFilters
}

export default function IssuesIndex({ issues, pagination, projects, filters }: IssuesIndexProps) {
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

  const getFilterUrl = (newFilters: Partial<IssueFilters>) => issuesFilterQuery(filters, newFilters)

  const visitFilters = (newFilters: Partial<IssueFilters>) =>
    router.get(getFilterUrl(newFilters), {}, { preserveState: true, replace: true, preserveScroll: true })

  const visitPage = (page: number) =>
    router.get(getFilterUrl({}), page > 1 ? { page } : {}, { preserveState: true })

  const projectOptions: Record<string, string> = {
    all: 'All projects',
    ...Object.fromEntries(projects.map(p => [p.id.toString(), p.name]))
  }

  return (
    <DashboardLayout>
      <PageHeader title="Issues" />
      <div className="flex flex-col h-full space-y-4">

        <div className="flex items-center gap-2 w-full">
          <div className="flex items-center border">
            <Select
              value={filters.project_id}
              onValueChange={(val) => visitFilters({ project_id: val || 'all' })}
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
            <Select
              value={filters.range}
              onValueChange={(val) => visitFilters({ range: val || '14d' })}
              items={TIME_RANGE_OPTIONS}
            >
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
              placeholder="Search Issues…"
              value={searchTerm}
              onChange={(e) => setSearchTerm(e.target.value)}
            />
          </div>

          <div className="flex items-center gap-2 shrink-0">
            <Select
              value={filters.sort}
              onValueChange={(val) => visitFilters({ sort: val || 'last_seen' })}
              items={SORT_OPTIONS}
            >
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

        <div className="border">
        <Table className="table-fixed">
          <TableHeader>
            <TableRow className="hover:bg-transparent">
              <TableHead className="w-[48px] px-4">
                <div className="flex items-center justify-center">
                  <Checkbox id="select-all" />
                </div>
              </TableHead>
              <TableHead>Issue</TableHead>
              <TableHead className="text-right px-4 w-32">Last seen</TableHead>
              <TableHead className="text-right px-4 w-32">Age</TableHead>
              <TableHead className="text-right px-4 w-24">Events</TableHead>
              <TableHead className="text-right px-4 w-24">Users</TableHead>
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
                <TableRow key={issue.id} className="group cursor-pointer">
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
                          <h3 className="font-semibold truncate text-foreground group-hover:underline">
                            {issue.title}
                          </h3>
                          <span className="font-mono text-xs text-muted-foreground truncate font-normal">
                            {issue.culprit}
                          </span>
                        </div>
                        <div className="flex items-center gap-2 text-xs text-muted-foreground">
                          <Badge className="font-mono">{issue.kind || 'error'}</Badge>
                          {issue.status === 'resolved' && <Badge variant="ok" dot>Resolved</Badge>}
                          <span>{issue.project.name}</span>
                        </div>
                      </div>
                    </Link>
                  </TableCell>
                  <TableCell className="text-right px-4 py-3 text-sm text-foreground/80 tabular-nums">
                    {formatDistanceToNow(new Date(issue.last_seen_at || issue.created_at), { addSuffix: false }).replace('about ', '')}
                  </TableCell>
                  <TableCell className="text-right px-4 py-3 text-sm text-muted-foreground tabular-nums">
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

        {pagination.total_pages > 1 && (
          <div className="flex items-center justify-between px-1">
            <div className="text-xs text-muted-foreground tabular-nums">
              Page {pagination.current_page} of {pagination.total_pages} · {pagination.total_count} issues
            </div>
            <div className="flex items-center gap-1">
              <Button
                variant="outline"
                disabled={pagination.current_page <= 1}
                onClick={() => visitPage(pagination.current_page - 1)}
              >
                <ArrowLeft className="h-3 w-3 mr-1" /> Previous
              </Button>
              <Button
                variant="outline"
                disabled={pagination.current_page >= pagination.total_pages}
                onClick={() => visitPage(pagination.current_page + 1)}
              >
                Next <ArrowRight className="h-3 w-3 ml-1" />
              </Button>
            </div>
          </div>
        )}
      </div>
    </DashboardLayout>
  )
}
