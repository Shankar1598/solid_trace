import { useState, useEffect } from 'react'
import { router, usePage } from '@inertiajs/react'
import DashboardLayout from '@/components/layouts/DashboardLayout'
import Breadcrumbs from '@/components/Breadcrumbs'
import { OverviewTabContent } from './components/OverviewTabContent'
import { EventsTabContent } from './components/EventsTabContent'
import { IssueSidebar } from './components/IssueSidebar'
import CommentList from './components/CommentList'
import { Button } from '@/components/ui/button'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Badge } from '@/components/ui/badge'
import { Separator } from '@/components/ui/separator'
import { Issue, Event, Comment, SharedProps } from '@/types'
import { CheckCircle2, XCircle } from 'lucide-react'

interface IssuesShowProps {
  issue: Issue
  event: Event | null
  prev_event_id: number | null
  next_event_id: number | null
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
  comments,
  current_environment,
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
    router.put(`/${current_org?.slug}/projects/${issue.project.slug}/issues/${issue.number}/resolve`)
  }

  const handleUnresolve = () => {
    router.put(`/${current_org?.slug}/projects/${issue.project.slug}/issues/${issue.number}/unresolve`)
  }

  if (!current_org) return null

  return (
    <DashboardLayout>
      <div className="space-y-6">
        {/* Header Section */}
        <div className="space-y-4">
          <Breadcrumbs
            organization={current_org}
            project={issue.project}
            issue={{ number: issue.number, title: issue.title }}
          />

          <div className="flex flex-col md:flex-row md:items-center justify-between gap-4">
            <div className="space-y-1">
              <h1 className="text-2xl font-bold tracking-tight">{issue.title}</h1>
              <p className="text-muted-foreground font-mono text-sm">{issue.culprit}</p>
            </div>
            <div className="flex items-center gap-2">
              <Badge variant={issue.status === 'resolved' ? 'secondary' : 'destructive'} className="uppercase tracking-wider text-[10px] py-0.5">
                {issue.status}
              </Badge>
              <Separator orientation="vertical" className="h-4 mx-2" />
              {issue.status === 'resolved' ? (
                <Button onClick={handleUnresolve} variant="outline" size="sm" className="gap-2">
                  <XCircle className="h-4 w-4 text-red-500" />
                  Unresolve
                </Button>
              ) : (
                <Button onClick={handleResolve} variant="outline" size="sm" className="gap-2">
                  <CheckCircle2 className="h-4 w-4 text-green-500" />
                  Resolve
                </Button>
              )}
            </div>
          </div>
        </div>

        <div className="grid grid-cols-1 lg:grid-cols-12 gap-6">
          {/* Main Content (Tabs) */}
          <div className="lg:col-span-8 space-y-6">
            <Tabs value={activeTab} onValueChange={handleTabChange} className="w-full">
              <TabsList variant="line" className="w-full justify-start border-b bg-transparent h-auto p-0 space-x-0">
                <TabsTrigger
                  value="overview"
                  className="data-active:bg-transparent px-6 py-3 text-sm font-medium transition-none after:bg-primary after:bottom-[-1px] data-active:after:opacity-100"
                >
                  Overview
                </TabsTrigger>
                <TabsTrigger
                  value="events"
                  className="data-active:bg-transparent px-6 py-3 text-sm font-medium transition-none after:bg-primary after:bottom-[-1px] data-active:after:opacity-100"
                >
                  Events
                </TabsTrigger>
                <TabsTrigger
                  value="comments"
                  className="data-active:bg-transparent px-6 py-3 text-sm font-medium transition-none after:bg-primary after:bottom-[-1px] data-active:after:opacity-100"
                >
                  Comments
                  {comments.length > 0 && (
                    <Badge variant="secondary" className="ml-2 px-1.5 py-0 text-[10px]">
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
                  current_environment={current_environment}
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
            <IssueSidebar issue={issue} />
          </div>
        </div>
      </div>
    </DashboardLayout>
  )
}
