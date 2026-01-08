import { router } from '@inertiajs/react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogTrigger } from '@/components/ui/dialog'
import { Hash, ArrowLeft, ArrowRight, Code, Copy, User as UserIcon, Tag } from 'lucide-react'
import { format, formatDistanceToNow } from 'date-fns'
import Stacktrace from '@/components/Stacktrace'
import { Issue, Event } from '@/types'
import { BreadcrumbsList } from './BreadcrumbsList'

interface OverviewTabContentProps {
  issue: Issue
  event: Event | null
  prev_event_id: number | null
  next_event_id: number | null
  current_org: { slug: string }
  current_environment: string
}

export function OverviewTabContent({
  issue,
  event,
  prev_event_id,
  next_event_id,
  current_org,
  current_environment
}: OverviewTabContentProps) {
  return (
    <div className="space-y-6">
      <Card>
        <div className="pb-4 px-4 flex items-center justify-between border-b">
          <div className="flex items-center gap-4">
            {event && (
              <div className="flex items-center gap-2 text-xs font-mono text-muted-foreground">
                <Hash className="h-3 w-3" />
                <span>{event.id}</span>
              </div>
            )}
          </div>
          <div className="flex items-center gap-1">
            <div className="text-xs text-muted-foreground mr-4">
              {event ? format(new Date(event.created_at), 'MMM d, yyyy HH:mm:ss') : 'N/A'}
              {event && ` (${formatDistanceToNow(new Date(event.created_at), { addSuffix: true })})`}
            </div>
            <Button
              variant="ghost"
              size="icon"
              disabled={!prev_event_id}
              className="h-8 w-8 border"
              onClick={() => prev_event_id && router.visit(
                `/${current_org.slug}/projects/${issue.project.slug}/issues/${issue.number}?event_id=${prev_event_id}&environment=${current_environment}`,
                { preserveScroll: true }
              )}
            >
              <ArrowLeft className="h-4 w-4" />
            </Button>
            <Button
              variant="ghost"
              size="icon"
              disabled={!next_event_id}
              className="h-8 w-8 border border-l-0"
              onClick={() => next_event_id && router.visit(
                `/${current_org.slug}/projects/${issue.project.slug}/issues/${issue.number}?event_id=${next_event_id}&environment=${current_environment}`,
                { preserveScroll: true }
              )}
            >
              <ArrowRight className="h-4 w-4" />
            </Button>

            <Dialog>
              <DialogTrigger render={<Button variant="outline" size="sm" className="h-8 gap-2 ml-2" />}>
                <div className="flex items-center gap-2">
                  <Code className="h-3.5 w-3.5" />
                  JSON
                </div>
              </DialogTrigger>
              <DialogContent className="max-w-4xl max-h-[80vh] flex flex-col">
                <DialogHeader className="flex flex-row items-center justify-between space-y-0 pb-4 border-b">
                  <DialogTitle>Event JSON</DialogTitle>
                  <Button
                    variant="ghost"
                    size="icon"
                    className="h-8 w-8"
                    onClick={() => {
                      if (event?.event_data) {
                        navigator.clipboard.writeText(JSON.stringify(event.event_data, null, 2))
                      }
                    }}
                  >
                    <Copy className="h-4 w-4" />
                  </Button>
                </DialogHeader>
                <div className="flex-1 overflow-auto bg-muted p-4 mt-4 border border-muted-foreground/20">
                  <pre className="text-xs font-mono">
                    {JSON.stringify(event?.event_data, null, 2)}
                  </pre>
                </div>
              </DialogContent>
            </Dialog>
          </div>
        </div>

        <CardContent className="p-6">
          {event && event.event_data && (event.event_data as any).exception ? (
            <Tabs defaultValue="relevant" className="w-full">
              <div className="flex items-center justify-between mb-4">
                <h3 className="text-sm font-semibold uppercase tracking-wider text-muted-foreground">Exception</h3>
                <TabsList className="bg-muted h-9 p-1">
                  <TabsTrigger value="relevant" className="text-xs px-3 data-active:bg-background data-active:shadow-none border border-transparent data-active:border-input">
                    Most relevant
                  </TabsTrigger>
                  <TabsTrigger value="full" className="text-xs px-3 data-active:bg-background data-active:shadow-none border border-transparent data-active:border-input">
                    Full stack trace
                  </TabsTrigger>
                </TabsList>
              </div>
              <TabsContent value="relevant" className="mt-0">
                <Stacktrace
                  frames={(event.event_data as any).exception.values[0].stacktrace.frames.filter((frame: any) => frame.in_app)}
                />
                {(event.event_data as any).exception.values[0].stacktrace.frames.filter((frame: any) => frame.in_app).length === 0 && (
                  <div className="text-center p-12 text-muted-foreground bg-muted/20 border border-dashed border-muted-foreground/30">
                    No application frames found.
                  </div>
                )}
              </TabsContent>
              <TabsContent value="full" className="mt-0">
                <Stacktrace frames={(event.event_data as any).exception.values[0].stacktrace.frames} />
              </TabsContent>
            </Tabs>
          ) : (
            <div className="text-center py-12 text-muted-foreground italic">
              No exception data available for this event.
            </div>
          )}
        </CardContent>
      </Card>

      {event && event.event_data && (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
          <Card>
            <CardHeader className="border-b">
              <div className="flex items-center gap-2">
                <UserIcon className="h-4 w-4 text-muted-foreground" />
                <CardTitle className="text-xs font-bold uppercase tracking-wider">User Context</CardTitle>
              </div>
            </CardHeader>
            <CardContent className="p-4">
              <pre className="text-[11px] font-mono bg-muted/50 p-4 border overflow-x-auto">
                {JSON.stringify((event.event_data as any).user || {}, null, 2)}
              </pre>
            </CardContent>
          </Card>

          <Card className="border-muted shadow-none">
            <CardHeader className="py-3 px-4 bg-muted/30 border-b flex flex-row items-center gap-2 space-y-0">
              <Tag className="h-4 w-4 text-muted-foreground" />
              <CardTitle className="text-xs font-bold uppercase tracking-wider">Tags</CardTitle>
            </CardHeader>
            <CardContent className="p-4">
              <div className="grid grid-cols-1 gap-1">
                {Object.entries((event.event_data as any).tags || {}).map(([key, value]) => (
                  <div key={key} className="flex items-center text-[11px] font-mono">
                    <span className="text-muted-foreground w-20 shrink-0">{key}:</span>
                    <span className="bg-muted px-1.5 py-0.5 border">{String(value)}</span>
                  </div>
                ))}
                {Object.keys((event.event_data as any).tags || {}).length === 0 && (
                  <span className="text-xs text-muted-foreground italic text-center p-4">No tags</span>
                )}
              </div>
            </CardContent>
          </Card>
        </div>
      )}

      {/* Breadcrumbs Section */}
      {event && event.event_data && (event.event_data as any).breadcrumbs && (
        <BreadcrumbsList breadcrumbs={(event.event_data as any).breadcrumbs} />
      )}
    </div>
  )
}
