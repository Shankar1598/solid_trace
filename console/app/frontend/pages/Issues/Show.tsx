import { useState, useEffect } from 'react'
import { router, usePage } from '@inertiajs/react'
import DashboardLayout from '@/components/layouts/DashboardLayout'
import { PageHeader } from '@/components/PageHeader'
import { OverviewTabContent } from './components/OverviewTabContent'
import { EventsTabContent } from './components/EventsTabContent'
import { IssueSidebar } from './components/IssueSidebar'
import CommentList from './components/CommentList'
import { Button } from '@/components/ui/button'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Badge } from '@/components/ui/badge'
import { Issue, Event, Comment, SharedProps } from '@/types'
import { CheckCircle2, XCircle } from 'lucide-react'

/** A section tab: as wide as its label, its underline on the list's rule. */
const TAB = 'flex-none px-0 py-2.5 text-sm transition-none after:bottom-[-1px]'

interface IssuesShowProps {
  issue: Issue
  event: Event | null
  prev_event_id: number | null
  next_event_id: number | null
  /** Why EventStore can't read Events now; null when it can. */
  events_unavailable: string | null
  comments: Comment[]
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
  events_unavailable,
  comments,
  events_list,
  events_pagination
}: IssuesShowProps) {
  const { current_user, current_org } = usePage<SharedProps>().props
  const validTabs = ['overview', 'events', 'comments']

  const getInitialTab = () => {
    const hash = window.location.hash.replace('#', '')
    return validTabs.includes(hash) ? hash : 'overview'
  }

  const [activeTab, setActiveTab] = useState(getInitialTab())

  useEffect(() => {
    setActiveTab(getInitialTab())
  }, [usePage().url])

  const handleTabChange = (value: string) => {
    setActiveTab(value)
    window.history.replaceState(null, '', `#${value}`)
  }

  const handleResolve = () => {
    router.patch(`/${current_org?.slug}/projects/${issue.project.slug}/issues/${issue.number}/resolve`)
  }

  const handleUnresolve = () => {
    router.patch(`/${current_org?.slug}/projects/${issue.project.slug}/issues/${issue.number}/unresolve`)
  }

  if (!current_org) return null

  const breadcrumbItems = [
    { label: 'Issues', href: `/${current_org.slug}/issues` },
    { label: `#${issue.number}`, title: issue.title }
  ]

  return (
    <DashboardLayout>
      <PageHeader
        breadcrumbs={breadcrumbItems}
        title={issue.title}
        badge={issue.status === 'resolved' ? <Badge variant="ok" dot>Resolved</Badge> : <Badge variant="error" dot>Unresolved</Badge>}
        description={issue.culprit && <span className="font-mono text-xs">{issue.culprit}</span>}
        actions={issue.status === 'resolved' ? (
          <Button onClick={handleUnresolve} variant="outline">
            <XCircle className="h-4 w-4" />
            Unresolve
          </Button>
        ) : (
          <Button onClick={handleResolve} variant="outline">
            <CheckCircle2 className="h-4 w-4" />
            Resolve
          </Button>
        )}
      />
      <div className="space-y-6">
        <div className="grid grid-cols-1 lg:grid-cols-12 gap-6">
          {/* Main Content (Tabs) */}
          <div className="lg:col-span-8 space-y-6">
            {events_unavailable && (
              <div role="alert" className="border border-destructive/40 bg-destructive/10 p-3 text-sm text-destructive">
                {events_unavailable}
              </div>
            )}
            <Tabs value={activeTab} onValueChange={handleTabChange} className="w-full">
              <TabsList variant="line" className="w-full justify-start gap-5 border-b h-auto p-0">
                <TabsTrigger
                  value="overview"
                  className={TAB}
                >
                  Overview
                </TabsTrigger>
                <TabsTrigger
                  value="events"
                  className={TAB}
                >
                  Events
                </TabsTrigger>
                <TabsTrigger
                  value="comments"
                  className={TAB}
                >
                  Comments
                  {comments.length > 0 && (
                    <Badge variant="count">
                      {comments.length}
                    </Badge>
                  )}
                </TabsTrigger>
              </TabsList>

              <TabsContent value="overview" className="space-y-6 pt-6">
                <OverviewTabContent
                  issue={issue}
                  event={event}
                  prev_event_id={prev_event_id}
                  next_event_id={next_event_id}
                  current_org={current_org}
                />
              </TabsContent>

              <TabsContent value="events" className="mt-6">
                <EventsTabContent
                  issue={issue}
                  events_list={events_list}
                  event={event}
                  events_pagination={events_pagination}
                  current_org={current_org}
                />
              </TabsContent>

              <TabsContent value="comments" className="mt-6">
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

          {/* Sidebar */}
          <div className="lg:col-span-4 space-y-6">
            <IssueSidebar issue={issue} orgSlug={current_org.slug} />
          </div>
        </div>
      </div>
    </DashboardLayout>
  )
}
