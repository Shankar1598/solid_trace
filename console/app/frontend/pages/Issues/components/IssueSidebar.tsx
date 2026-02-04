import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Separator } from '@/components/ui/separator'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { router } from '@inertiajs/react'
import { Clock, Hash, MapPin } from 'lucide-react'
import { format, formatDistanceToNow } from 'date-fns'
import { Issue, User } from '@/types'

interface IssueSidebarProps {
  issue: Issue
  assignees: Array<{
    id: number
    user: User
  }>
  orgSlug: string
}

export function IssueSidebar({ issue, assignees, orgSlug }: IssueSidebarProps) {
  const assigneeValue = issue.assignee ? String(issue.assignee.id) : 'unassigned'
  const assigneeIsArchived = Boolean(issue.assignee?.discarded_at)
  const assigneeInList = issue.assignee ? assignees.some((a) => a.id === issue.assignee!.id) : true

  const selectedLabel =
    assigneeValue === 'unassigned'
      ? 'Unassigned'
      : (assignees.find((m) => String(m.id) === assigneeValue)?.user.name ?? issue.assignee?.user.name ?? assigneeValue)

  const updateAssignee = (value: string) => {
    router.patch(
      `/${orgSlug}/projects/${issue.project.slug}/issues/${issue.number}/assign`,
      { assignee_id: value === 'unassigned' ? null : Number(value) },
      { preserveScroll: true }
    )
  }

  return (
    <Card>
      <CardHeader className="border-b">
        <CardTitle className="uppercase">Issue Statistics</CardTitle>
      </CardHeader>
      <CardContent className="p-4 space-y-5">
        <div className="space-y-2">
          <span className="text-[10px] text-muted-foreground uppercase font-bold tracking-widest">
            Assigned to
          </span>
          <Select value={assigneeValue} onValueChange={updateAssignee}>
            <SelectTrigger>
              <div className="flex items-center justify-between w-full">
                <span className="truncate">{selectedLabel}</span>
                <SelectValue className="sr-only" />
              </div>
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="unassigned">Unassigned</SelectItem>
              {!assigneeInList && issue.assignee && (
                <SelectItem value={String(issue.assignee.id)} disabled>
                  {issue.assignee.user.name} (Archived)
                </SelectItem>
              )}
              {assignees.map((member) => (
                <SelectItem key={member.id} value={String(member.id)}>
                  {member.user.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          {assigneeIsArchived && issue.assignee && (
            <div className="text-[11px] text-muted-foreground">
              Currently assigned member is archived.
            </div>
          )}
        </div>

        <Separator />

        <div className="flex justify-between items-start">
          <div className="space-y-1">
            <span className="text-[10px] text-muted-foreground uppercase font-bold tracking-widest flex items-center gap-1.5">
              <Clock className="h-3 w-3" />
              First Seen
            </span>
            <div className="text-sm font-medium">{formatDistanceToNow(new Date(issue.created_at), { addSuffix: true })}</div>
            <div className="text-[10px] text-muted-foreground">{format(new Date(issue.created_at), 'MMM d, yyyy')}</div>
          </div>
        </div>

        <div className="flex justify-between items-start">
          <div className="space-y-1">
            <span className="text-[10px] text-muted-foreground uppercase font-bold tracking-widest flex items-center gap-1.5">
              <Clock className="h-3 w-3" />
              Last Seen
            </span>
            <div className="text-sm font-medium">{formatDistanceToNow(new Date(issue.last_seen_at), { addSuffix: true })}</div>
            <div className="text-[10px] text-muted-foreground">{format(new Date(issue.last_seen_at), 'MMM d, yyyy')}</div>
          </div>
        </div>

        <Separator />

        <div className="grid grid-cols-2 gap-4 pt-1">
          <div className="space-y-1">
            <span className="text-[10px] text-muted-foreground uppercase font-bold tracking-widest flex items-center gap-1.5">
              <Hash className="h-3 w-3" />
              Events
            </span>
            <div className="text-xl font-bold tracking-tighter">{issue.events_count}</div>
          </div>
          <div className="space-y-1">
            <span className="text-[10px] text-muted-foreground uppercase font-bold tracking-widest flex items-center gap-1.5">
              <MapPin className="h-3 w-3" />
              Project
            </span>
            <div className="text-sm font-medium truncate">{issue.project.name}</div>
          </div>
        </div>
      </CardContent>
    </Card>
  )
}
