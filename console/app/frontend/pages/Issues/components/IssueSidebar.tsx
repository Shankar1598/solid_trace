import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Separator } from '@/components/ui/separator'
import { Clock, Hash, MapPin } from 'lucide-react'
import { format, formatDistanceToNow } from 'date-fns'
import { Issue } from '@/types'

interface IssueSidebarProps {
  issue: Issue
}

export function IssueSidebar({ issue }: IssueSidebarProps) {
  return (
    <Card>
      <CardHeader className="border-b">
        <CardTitle className="uppercase">Issue Statistics</CardTitle>
      </CardHeader>
      <CardContent className="p-4 space-y-5">
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
