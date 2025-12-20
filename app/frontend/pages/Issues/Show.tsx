import { router, usePage } from '@inertiajs/react'
import DashboardLayout from '@/components/layouts/DashboardLayout'
import Breadcrumbs from '@/components/Breadcrumbs'
import Stacktrace from '@/components/Stacktrace'
import CommentList from '@/components/CommentList'
import { Card, CardContent, CardHeader } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Issue, Event, Comment, SharedProps } from '@/types'
import { ArrowLeft, ArrowRight, CheckCircle2, XCircle, Code, Copy } from 'lucide-react'
import { formatDistanceToNow, format } from 'date-fns'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog"

interface IssuesShowProps {
  issue: Issue
  event: Event | null
  prev_event_id: number | null
  next_event_id: number | null
  environments: string[]
  comments: Comment[]
  current_environment: string
  events_list: Event[]
  events_pagination: {
    current_page: number
    total_pages: number
    total_count: number
  }
}

export default function IssuesShow({
  issue,
  event,
  prev_event_id,
  next_event_id,
  environments,
  comments,
  current_environment,
  events_list,
  events_pagination
}: IssuesShowProps) {
  const { current_user, current_org } = usePage<SharedProps>().props

  const handleResolve = () => {
    router.put(`/${current_org?.slug}/projects/${issue.project.slug}/issues/${issue.number}/resolve`)
  }

  const handleUnresolve = () => {
    router.put(`/${current_org?.slug}/projects/${issue.project.slug}/issues/${issue.number}/unresolve`)
  }

  const handleEnvironmentChange = (env: string) => {
    router.visit(`/${current_org?.slug}/projects/${issue.project.slug}/issues/${issue.number}?environment=${env}`, {
      preserveScroll: true
    })
  }

  const handleEventsPageChange = (page: number) => {
    router.visit(`/${current_org?.slug}/projects/${issue.project.slug}/issues/${issue.number}?events_page=${page}`, {
      preserveScroll: true,
      only: ['events_list', 'events_pagination'] // Optimize partial reload
    })
  }

  if (!current_org) return null

  return (
    <DashboardLayout>
      <div className="space-y-6">
        <Breadcrumbs
          organization={current_org}
          project={issue.project}
          issue={{ number: issue.number, title: issue.title }}
        />

        <div className="flex items-start justify-between gap-4">
          <div className="space-y-1">
            <h1 className="text-2xl font-bold tracking-tight">{issue.title}</h1>
            <p className="text-muted-foreground">{issue.culprit}</p>
          </div>
          <div className="flex items-center gap-2">
            {issue.status === 'resolved' ? (
              <Button onClick={handleUnresolve} variant="outline" className="gap-2">
                <XCircle className="h-4 w-4 text-red-500" />
                Unresolve
              </Button>
            ) : (
              <Button onClick={handleResolve} variant="outline" className="gap-2">
                <CheckCircle2 className="h-4 w-4 text-green-500" />
                Resolve
              </Button>
            )}
          </div>
        </div>

        <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
          <div className="lg:col-span-2 space-y-6">
            <Tabs defaultValue="overview">
              <TabsList className="w-full justify-start border-b rounded-none h-auto p-0 bg-transparent">
                <TabsTrigger
                  value="overview"
                  className="rounded-none border-b-2 border-transparent data-[state=active]:border-primary data-[state=active]:bg-transparent data-[state=active]:shadow-none px-4 py-2"
                >
                  Overview
                </TabsTrigger>
                <TabsTrigger
                  value="events"
                  className="rounded-none border-b-2 border-transparent data-[state=active]:border-primary data-[state=active]:bg-transparent data-[state=active]:shadow-none px-4 py-2"
                >
                  Events
                </TabsTrigger>
                <TabsTrigger
                  value="comments"
                  id="comments"
                  className="rounded-none border-b-2 border-transparent data-[state=active]:border-primary data-[state=active]:bg-transparent data-[state=active]:shadow-none px-4 py-2"
                >
                  Comments
                  <span className="ml-2 rounded-full bg-muted px-2 py-0.5 text-xs">
                    {comments.length}
                  </span>
                </TabsTrigger>
              </TabsList>

              <TabsContent value="overview" className="space-y-6">
                {/* Event Navigation & Header Info */}
                <div className="flex flex-col gap-4">
                  <div className="flex items-center justify-between bg-card p-3 rounded-md border shadow-sm">
                    <div className="flex items-center gap-2">
                      <Select value={current_environment} onValueChange={handleEnvironmentChange}>
                        <SelectTrigger className="w-[180px] h-8 bg-background">
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

                    <div className="flex items-center gap-2 text-sm">
                      <Button
                        variant="ghost"
                        size="sm"
                        disabled={!prev_event_id}
                        className="h-8"
                        onClick={() => prev_event_id && router.visit(
                          `/${current_org.slug}/projects/${issue.project.slug}/issues/${issue.number}?event_id=${prev_event_id}&environment=${current_environment}`,
                          { preserveScroll: true }
                        )}
                      >
                        <ArrowLeft className="h-4 w-4 mr-1" /> Older
                      </Button>
                      <span className="text-muted-foreground text-xs font-medium bg-muted px-2 py-1 rounded">
                        {event ? formatDistanceToNow(new Date(event.created_at), { addSuffix: true }) : 'N/A'}
                      </span>
                      <Button
                        variant="ghost"
                        size="sm"
                        disabled={!next_event_id}
                        className="h-8"
                        onClick={() => next_event_id && router.visit(
                          `/${current_org.slug}/projects/${issue.project.slug}/issues/${issue.number}?event_id=${next_event_id}&environment=${current_environment}`,
                          { preserveScroll: true }
                        )}
                      >
                        Newer <ArrowRight className="h-4 w-4 ml-1" />
                      </Button>

                      <Dialog>
                        <DialogTrigger asChild>
                          <Button variant="outline" size="sm" className="h-8 gap-2 ml-2">
                            <Code className="h-3.5 w-3.5" />
                            View JSON
                          </Button>
                        </DialogTrigger>
                        <DialogContent className="max-w-4xl max-h-[80vh] flex flex-col">
                          <DialogHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
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
                              <span className="sr-only">Copy JSON</span>
                            </Button>
                          </DialogHeader>
                          <div className="flex-1 overflow-auto rounded-md bg-muted p-4">
                            <pre className="text-xs font-mono text-foreground">
                              {JSON.stringify(event?.event_data, null, 2)}
                            </pre>
                          </div>
                        </DialogContent>
                      </Dialog>
                    </div>
                  </div>

                  {event && (
                    <div className="space-y-4">
                      {/* Assuming event.event_data has python exception structure for now based on context */}
                      {event.event_data && (event.event_data as any).exception && (
                        <Tabs defaultValue="relevant" className="w-full">
                          <div className="flex items-center justify-end mb-2">
                            <TabsList className="grid w-[300px] grid-cols-2">
                              <TabsTrigger value="relevant">Most relevant</TabsTrigger>
                              <TabsTrigger value="full">Full stack trace</TabsTrigger>
                            </TabsList>
                          </div>
                          <TabsContent value="relevant">
                            <Stacktrace
                              frames={(event.event_data as any).exception.values[0].stacktrace.frames.filter((frame: any) => frame.in_app)}
                            />
                            {(event.event_data as any).exception.values[0].stacktrace.frames.filter((frame: any) => frame.in_app).length === 0 && (
                              <div className="text-center p-8 text-muted-foreground bg-muted/30 rounded-md border border-dashed">
                                No application frames found. Switch to full stack trace to see all frames.
                              </div>
                            )}
                          </TabsContent>
                          <TabsContent value="full">
                            <Stacktrace frames={(event.event_data as any).exception.values[0].stacktrace.frames} />
                          </TabsContent>
                        </Tabs>
                      )}

                      {/* Render other parts of event data... Tags, User context etc */}
                      <div className="grid gap-4 md:grid-cols-2">
                        <Card className="shadow-sm border-muted">
                          <CardHeader className="pb-2 border-b bg-muted/20">
                            <span className="text-sm font-semibold">User</span>
                          </CardHeader>
                          <CardContent className="text-sm pt-4">
                            <pre className="text-xs bg-muted/50 p-3 rounded-md overflow-x-auto border">
                              {JSON.stringify((event.event_data as any).user || {}, null, 2)}
                            </pre>
                          </CardContent>
                        </Card>

                        <Card className="shadow-sm border-muted">
                          <CardHeader className="pb-2 border-b bg-muted/20">
                            <span className="text-sm font-semibold">Tags</span>
                          </CardHeader>
                          <CardContent className="text-sm pt-4">
                            <pre className="text-xs bg-muted/50 p-3 rounded-md overflow-x-auto border">
                              {JSON.stringify((event.event_data as any).tags || [], null, 2)}
                            </pre>
                          </CardContent>
                        </Card>
                      </div>
                    </div>
                  )}
                </div>
              </TabsContent>

              <TabsContent value="events" className="mt-4">
                <Card className="shadow-sm">
                  <CardHeader className="pb-3 border-b bg-muted/20 rounded-t-lg">
                    <h3 className="font-semibold text-sm">Events</h3>
                  </CardHeader>
                  <CardContent className="p-0">
                    <div className="relative w-full overflow-auto">
                      <table className="w-full caption-bottom text-sm text-left">
                        <thead className="[&_tr]:border-b">
                          <tr className="border-b transition-colors hover:bg-muted/50 data-[state=selected]:bg-muted">
                            <th className="h-10 px-4 align-middle font-medium text-muted-foreground">ID</th>
                            <th className="h-10 px-4 align-middle font-medium text-muted-foreground">Time</th>
                            <th className="h-10 px-4 align-middle font-medium text-muted-foreground">Environment</th>
                          </tr>
                        </thead>
                        <tbody className="[&_tr:last-child]:border-0">
                          {events_list.map((evt) => (
                            <tr
                              key={evt.id}
                              className="border-b transition-colors hover:bg-muted/50 data-[state=selected]:bg-muted cursor-pointer"
                              onClick={() => router.visit(`/${current_org?.slug}/projects/${issue.project.slug}/issues/${issue.number}?event_id=${evt.id}`, { preserveScroll: true })}
                            >
                              <td className="p-4 align-middle font-mono">{evt.id}</td>
                              <td className="p-4 align-middle">
                                {format(new Date(evt.created_at), 'MMM d, yyyy HH:mm:ss')}
                              </td>
                              <td className="p-4 align-middle">
                                <span className="inline-flex items-center rounded-md bg-gray-50 px-2 py-1 text-xs font-medium text-gray-600 ring-1 ring-inset ring-gray-500/10">
                                  {evt.environment}
                                </span>
                              </td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    </div>

                    {events_pagination.total_pages > 1 && (
                      <div className="flex items-center justify-between px-4 py-4 border-t">
                        <div className="text-xs text-muted-foreground">
                          Page {events_pagination.current_page} of {events_pagination.total_pages}
                        </div>
                        <div className="flex items-center gap-2">
                          <Button
                            variant="outline"
                            size="sm"
                            disabled={events_pagination.current_page <= 1}
                            onClick={() => handleEventsPageChange(events_pagination.current_page - 1)}
                          >
                            <ArrowLeft className="h-4 w-4 mr-1" /> Previous
                          </Button>
                          <Button
                            variant="outline"
                            size="sm"
                            disabled={events_pagination.current_page >= events_pagination.total_pages}
                            onClick={() => handleEventsPageChange(events_pagination.current_page + 1)}
                          >
                            Next <ArrowRight className="h-4 w-4 ml-1" />
                          </Button>
                        </div>
                      </div>
                    )}
                  </CardContent>
                </Card>
              </TabsContent>

              <TabsContent value="comments">
                <CommentList
                  comments={comments}
                  currentUser={current_user}
                  issueId={issue.number}
                  projectId={issue.project.slug}
                  orgSlug={current_org.slug}
                />
              </TabsContent>
            </Tabs>
          </div>

          <div className="space-y-6">
            <Card className="shadow-sm border-muted">
              <CardHeader className="pb-3 border-b bg-muted/20 rounded-t-lg">
                <h3 className="font-semibold text-sm">Details</h3>
              </CardHeader>
              <CardContent className="pt-4 space-y-4 text-sm">
                <div>
                  <span className="text-muted-foreground block mb-1">First Seen</span>
                  <span>{formatDistanceToNow(new Date(issue.created_at), { addSuffix: true })}</span>
                </div>
                <div>
                  <span className="text-muted-foreground block mb-1">Last Seen</span>
                  <span>{formatDistanceToNow(new Date(issue.last_seen_at), { addSuffix: true })}</span>
                </div>
                <div>
                  <span className="text-muted-foreground block mb-1">Total Events</span>
                  <span>{issue.events_count}</span>
                </div>
              </CardContent>
            </Card>
          </div>
        </div>
      </div>
    </DashboardLayout >
  )
}
