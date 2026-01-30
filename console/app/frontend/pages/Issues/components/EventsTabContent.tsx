import { router } from '@inertiajs/react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { ArrowLeft, ArrowRight } from 'lucide-react'
import { format } from 'date-fns'
import { Issue, Event } from '@/types'
import { cn } from '@/lib/utils'

interface EventsTabContentProps {
  issue: Issue
  events_list: Event[]
  event: Event | null
  events_pagination: {
    current_page: number
    total_pages: number
  }
  current_org: {
    slug: string
  }
}

export function EventsTabContent({
  issue,
  events_list,
  event,
  events_pagination,
  current_org
}: EventsTabContentProps) {
  const handleEventsPageChange = (page: number) => {
    router.visit(`/${current_org?.slug}/projects/${issue.project.slug}/issues/${issue.number}?events_page=${page}`, {
      preserveScroll: true,
      only: ['events_list', 'events_pagination']
    })
  }

  return (
    <Card>
      <CardHeader className="border-b px-4">
        <CardTitle className="text-[10px] font-bold uppercase tracking-wider">Historical Events</CardTitle>
      </CardHeader>
      <CardContent className="p-0">
        <Table>
          <TableHeader className="bg-muted/10">
            <TableRow className="hover:bg-transparent border-b">
              <TableHead className="w-[100px] text-[10px] font-bold uppercase tracking-wider py-3 px-4">ID</TableHead>
              <TableHead className="text-[10px] font-bold uppercase tracking-wider py-3 px-4">Timestamp</TableHead>
              <TableHead className="text-[10px] font-bold uppercase tracking-wider py-3 px-4">Environment</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {events_list.map((evt) => (
              <TableRow
                key={evt.id}
                className={cn(
                  "cursor-pointer transition-colors border-b",
                  event?.id === evt.id ? "bg-muted/60" : "hover:bg-muted/30"
                )}
                onClick={() => router.visit(`/${current_org?.slug}/projects/${issue.project.slug}/issues/${issue.number}?event_id=${evt.id}`, { preserveScroll: true })}
              >
                <TableCell className="font-mono text-xs py-3 px-4">{evt.id}</TableCell>
                <TableCell className="text-xs py-3 px-4">{format(new Date(evt.created_at), 'MMM d, yyyy HH:mm:ss')}</TableCell>
                <TableCell className="py-3 px-4">
                  <Badge variant="outline" className="px-2 py-0 text-[10px] uppercase font-mono tracking-tight bg-background">
                    {evt.environment}
                  </Badge>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>

        {events_pagination.total_pages > 1 && (
          <div className="flex items-center justify-between px-6 py-4 border-t bg-muted/10">
            <div className="text-[11px] text-muted-foreground font-medium">
              Page {events_pagination.current_page} of {events_pagination.total_pages}
            </div>
            <div className="flex items-center gap-1">
              <Button
                variant="outline"
                size="sm"
                className="h-8 text-[11px]"
                disabled={events_pagination.current_page <= 1}
                onClick={() => handleEventsPageChange(events_pagination.current_page - 1)}
              >
                <ArrowLeft className="h-3 w-3 mr-1" /> Previous
              </Button>
              <Button
                variant="outline"
                size="sm"
                className="h-8 text-[11px]"
                disabled={events_pagination.current_page >= events_pagination.total_pages}
                onClick={() => handleEventsPageChange(events_pagination.current_page + 1)}
              >
                Next <ArrowRight className="h-3 w-3 ml-1" />
              </Button>
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  )
}
