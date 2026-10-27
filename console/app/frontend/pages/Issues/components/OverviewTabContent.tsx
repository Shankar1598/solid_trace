import { Link, router } from '@inertiajs/react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Dialog, DialogClose, DialogContent, DialogHeader, DialogTitle, DialogTrigger } from '@/components/ui/dialog'
import { Hash, ArrowLeft, ArrowRight, Code, Copy, User as UserIcon, Tag, Waypoints, X } from 'lucide-react'
import { format, formatDistanceToNow } from 'date-fns'
import Stacktrace from '@/components/Stacktrace'
import { TRACE_ID, tracePath } from '@/lib/traces'
import { Issue, Event } from '@/types'
import { BreadcrumbsList } from './BreadcrumbsList'

/** The id of the trace the Event was raised in, from its trace context, or null. */
function traceIdOf(event: Event): string | null {
  const id = (event.event_data as { contexts?: { trace?: { trace_id?: unknown } } } | null)?.contexts?.trace?.trace_id
  return typeof id === 'string' && TRACE_ID.test(id) ? id : null
}

interface OverviewTabContentProps {
  issue: Issue
  event: Event | null
  prev_event_id: number | null
  next_event_id: number | null
  current_org: { slug: string }
}

/** The frames switch's tabs, drawn as the app's segmented switches are. */
const FRAMES_TAB = 'h-full flex-none border-0 border-l px-2.5 text-xs first:border-l-0 data-active:bg-primary data-active:text-primary-foreground dark:data-active:bg-primary dark:data-active:text-primary-foreground dark:data-active:border-transparent'

export function OverviewTabContent({
  issue,
  event,
  prev_event_id,
  next_event_id,
  current_org,
}: OverviewTabContentProps) {
  const formattedEventJson = event?.event_data ? JSON.stringify(event.event_data, null, 2) : ''
  const handleCopyEventJson = () => {
    if (formattedEventJson) {
      navigator.clipboard.writeText(formattedEventJson)
    }
  }

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
            {event && traceIdOf(event) && (
              <Link
                href={tracePath(current_org.slug, traceIdOf(event)!, event.created_at)}
                className="inline-flex h-7 items-center gap-1.5 border px-2 text-xs text-muted-foreground hover:bg-muted hover:text-foreground"
              >
                <Waypoints className="h-3.5 w-3.5" /> Open trace
              </Link>
            )}
          </div>
          <div className="flex items-center gap-1">
            <div className="text-xs text-muted-foreground mr-4">
              {event ? <span className="font-mono">{format(new Date(event.created_at), 'MMM d, yyyy HH:mm:ss')}</span> : 'N/A'}
              {event && ` (${formatDistanceToNow(new Date(event.created_at), { addSuffix: true })})`}
            </div>
            <Button
              variant="ghost"
              size="icon"
              disabled={!prev_event_id}
              aria-label="Previous event"
              title="Previous event"
              className="h-8 w-8 border"
              onClick={() => prev_event_id && router.visit(
                `/${current_org.slug}/projects/${issue.project.slug}/issues/${issue.number}?event_id=${prev_event_id}`,
                { preserveScroll: true }
              )}
            >
              <ArrowLeft className="h-4 w-4" />
            </Button>
            <Button
              variant="ghost"
              size="icon"
              disabled={!next_event_id}
              aria-label="Next event"
              title="Next event"
              className="h-8 w-8 border border-l-0"
              onClick={() => next_event_id && router.visit(
                `/${current_org.slug}/projects/${issue.project.slug}/issues/${issue.number}?event_id=${next_event_id}`,
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
              <DialogContent className="w-[80vw] max-w-[80vw] sm:max-w-[80vw] max-h-[80vh] flex flex-col" showCloseButton={false}>
                <DialogHeader className="flex flex-row items-center justify-between space-y-0 pb-4 border-b">
                  <DialogTitle>Event JSON</DialogTitle>
                  <div className="flex items-center gap-2">
                    <Button
                      variant="outline"
                      size="sm"
                      className="h-8 gap-2"
                      disabled={!formattedEventJson}
                      onClick={handleCopyEventJson}
                    >
                      <Copy className="h-3.5 w-3.5" />
                      Copy
                    </Button>
                    <DialogClose
                      render={<Button variant="ghost" size="icon-sm" className="h-8 w-8" />}
                    >
                      <X className="h-4 w-4" />
                      <span className="sr-only">Close</span>
                    </DialogClose>
                  </div>
                </DialogHeader>
                <div className="flex-1 overflow-auto bg-muted/40 p-4 mt-4 border border-muted-foreground/20">
                  {formattedEventJson ? (
                    <pre className="text-xs font-mono text-foreground whitespace-pre-wrap">
                      {formattedEventJson}
                    </pre>
                  ) : (
                    <div className="text-sm text-muted-foreground italic">
                      No event data available.
                    </div>
                  )}
                </div>
              </DialogContent>
            </Dialog>
          </div>
        </div>

        <CardContent className="p-6">
          {event && event.event_data && (event.event_data as any).exception ? (
            <Tabs defaultValue="relevant" className="w-full">
              <div className="flex items-center justify-between mb-4">
                <h3 className="text-sm font-semibold">Exception</h3>
                <TabsList className="h-8 gap-0 border bg-transparent p-0">
                  <TabsTrigger value="relevant" className={FRAMES_TAB}>
                    Most relevant
                  </TabsTrigger>
                  <TabsTrigger value="full" className={FRAMES_TAB}>
                    Full stack trace
                  </TabsTrigger>
                </TabsList>
              </div>
              <TabsContent value="relevant" className="mt-0">
                <Stacktrace
                  frames={((event.event_data as any).exception?.values?.[0]?.stacktrace?.frames || []).filter((frame: any) => frame.in_app)}
                />
                {((event.event_data as any).exception?.values?.[0]?.stacktrace?.frames || []).filter((frame: any) => frame.in_app).length === 0 && (
                  <div className="text-center p-12 text-muted-foreground bg-muted/20 border border-dashed border-muted-foreground/30">
                    No application frames found.
                  </div>
                )}
              </TabsContent>
              <TabsContent value="full" className="mt-0">
                <Stacktrace frames={(event.event_data as any).exception?.values?.[0]?.stacktrace?.frames || []} />
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
                <CardTitle>User context</CardTitle>
              </div>
            </CardHeader>
            <CardContent className="p-4">
              <pre className="text-[11px] font-mono bg-muted/50 p-4 border overflow-x-auto">
                {JSON.stringify((event.event_data as any).user || {}, null, 2)}
              </pre>
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="border-b">
              <div className="flex items-center gap-2">
                <Tag className="h-4 w-4 text-muted-foreground" />
                <CardTitle>Tags</CardTitle>
              </div>
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
