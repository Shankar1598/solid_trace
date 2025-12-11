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
import { ArrowLeft, ArrowRight, CheckCircle2, XCircle } from 'lucide-react'
import { formatDistanceToNow } from 'date-fns'

interface IssuesShowProps {
  issue: Issue
  event: Event | null
  prev_event_id: number | null
  next_event_id: number | null
  environments: string[]
  comments: Comment[]
  current_environment: string
}

export default function IssuesShow({
  issue,
  event,
  prev_event_id,
  next_event_id,
  environments,
  comments,
  current_environment
}: IssuesShowProps) {
  const { current_user, current_org } = usePage<SharedProps>().props

  const handleResolve = () => {
    router.put(`/${current_org?.slug}/issues/${issue.number}/resolve`)
  }

  const handleUnresolve = () => {
    router.put(`/${current_org?.slug}/issues/${issue.number}/unresolve`)
  }

  const handleEnvironmentChange = (env: string) => {
    router.visit(`/${current_org?.slug}/issues/${issue.number}?environment=${env}`, {
      preserveScroll: true
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
              <TabsList>
                <TabsTrigger value="overview">Overview</TabsTrigger>
                <TabsTrigger value="json">JSON</TabsTrigger>
                <TabsTrigger value="comments" id="comments">
                  Comments
                  <span className="ml-2 rounded-full bg-muted px-2 py-0.5 text-xs">
                    {comments.length}
                  </span>
                </TabsTrigger>
              </TabsList>

              <TabsContent value="overview" className="space-y-6">
                {/* Event Navigation */}
                <div className="flex items-center justify-between bg-muted/30 p-2 rounded-md border">
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
                      onClick={() => prev_event_id && router.visit(
                        `/${current_org.slug}/issues/${issue.number}?event_id=${prev_event_id}&environment=${current_environment}`,
                        { preserveScroll: true }
                      )}
                    >
                      <ArrowLeft className="h-4 w-4 mr-1" /> Older
                    </Button>
                    <span className="text-muted-foreground text-xs">
                      {event ? formatDistanceToNow(new Date(event.created_at), { addSuffix: true }) : 'N/A'}
                    </span>
                    <Button
                      variant="ghost"
                      size="sm"
                      disabled={!next_event_id}
                      onClick={() => next_event_id && router.visit(
                        `/${current_org.slug}/issues/${issue.number}?event_id=${next_event_id}&environment=${current_environment}`,
                        { preserveScroll: true }
                      )}
                    >
                      Newer <ArrowRight className="h-4 w-4 ml-1" />
                    </Button>
                  </div>
                </div>

                {event && (
                  <div className="space-y-4">
                    {/* Assuming event.event_data has python exception structure for now based on context */}
                    {/* We would render Stacktrace here if the event data matches */}
                    {event.event_data && (event.event_data as any).exception && (
                      <Stacktrace frames={(event.event_data as any).exception.values[0].stacktrace.frames} />
                    )}

                    {/* Render other parts of event data... Tags, User context etc */}
                    <div className="grid gap-4 md:grid-cols-2">
                      <Card>
                        <CardHeader className="pb-2">
                          <span className="text-sm font-semibold">User</span>
                        </CardHeader>
                        <CardContent className="text-sm">
                          {/* Placeholder for user data */}
                          <pre className="text-xs bg-muted p-2 rounded overflow-x-auto">
                            {JSON.stringify((event.event_data as any).user || {}, null, 2)}
                          </pre>
                        </CardContent>
                      </Card>

                      <Card>
                        <CardHeader className="pb-2">
                          <span className="text-sm font-semibold">Tags</span>
                        </CardHeader>
                        <CardContent className="text-sm">
                          {/* Placeholder for tags */}
                          <pre className="text-xs bg-muted p-2 rounded overflow-x-auto">
                            {JSON.stringify((event.event_data as any).tags || [], null, 2)}
                          </pre>
                        </CardContent>
                      </Card>
                    </div>
                  </div>
                )}
              </TabsContent>

              <TabsContent value="json">
                <Card>
                  <CardContent className="p-0">
                    <pre className="p-4 text-xs font-mono overflow-x-auto bg-slate-950 text-slate-50 rounded-lg">
                      {JSON.stringify(event?.event_data, null, 2)}
                    </pre>
                  </CardContent>
                </Card>
              </TabsContent>

              <TabsContent value="comments">
                <CommentList
                  comments={comments}
                  currentUser={current_user}
                  issueId={issue.number}
                  orgSlug={current_org.slug}
                />
              </TabsContent>
            </Tabs>
          </div>

          <div className="space-y-6">
            <Card>
              <CardHeader className="pb-3 border-b">
                <h3 className="font-semibold text-sm">Details</h3>
              </CardHeader>
              <CardContent className="pt-4 space-y-4 text-sm">
                <div>
                  <span className="text-muted-foreground block mb-1">First Seen</span>
                  <span>{formatDistanceToNow(new Date(issue.created_at), { addSuffix: true })}</span>
                </div>
                <div>
                  <span className="text-muted-foreground block mb-1">Last Seen</span>
                  <span>{formatDistanceToNow(new Date(issue.updated_at), { addSuffix: true })}</span>
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
    </DashboardLayout>
  )
}
