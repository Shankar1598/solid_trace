import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Separator } from '@/components/ui/separator'
import {
  Combobox,
  ComboboxContent,
  ComboboxInput,
  ComboboxItem,
  ComboboxList,
} from '@/components/ui/combobox'
import { router } from '@inertiajs/react'
import { Clock, Hash, MapPin } from 'lucide-react'
import { format, formatDistanceToNow } from 'date-fns'
import { Issue, User } from '@/types'
import { toast } from 'sonner'
import { useEffect, useMemo, useState } from 'react'

type OrganizationMember = {
  id: number
  user: User
  discarded_at?: string | null
}

interface IssueSidebarProps {
  issue: Issue
  orgSlug: string
}

export function IssueSidebar({ issue, orgSlug }: IssueSidebarProps) {
  const assigneeValue = issue.assignee ? String(issue.assignee.id) : 'unassigned'
  const assigneeIsArchived = Boolean(issue.assignee?.discarded_at)
  const [members, setMembers] = useState<OrganizationMember[]>([])
  const [query, setQuery] = useState('')
  const [isLoading, setIsLoading] = useState(false)

  const selectedLabel = useMemo(() => {
    if (assigneeValue === 'unassigned') return 'Unassigned'
    const selected = members.find((m) => String(m.id) === assigneeValue)
    return selected?.user.name ?? issue.assignee?.user.name ?? issue.assignee?.user.email ?? assigneeValue
  }, [assigneeValue, members, issue.assignee])

  const assigneeInList = issue.assignee ? members.some((a) => a.id === issue.assignee!.id) : true

  const mergedMembers = useMemo(() => {
    if (!issue.assignee || assigneeInList) return members
    return [
      {
        id: issue.assignee.id,
        user: issue.assignee.user,
        discarded_at: issue.assignee.discarded_at,
      },
      ...members,
    ]
  }, [assigneeInList, issue.assignee, members])

  const updateAssignee = (value: string | null) => {
    const nextValue = value ?? 'unassigned'

    router.patch(
      `/${orgSlug}/projects/${issue.project.slug}/issues/${issue.number}/assign`,
      { assignee_id: nextValue === 'unassigned' ? null : Number(nextValue) },
      {
        preserveScroll: true,
        onError: () => {
          toast.error('Failed to update assignee')
        },
      }
    )
  }

  useEffect(() => {
    let isActive = true
    const controller = new AbortController()
    const trimmedQuery = query.trim()

    const timeout = window.setTimeout(async () => {
      setIsLoading(true)
      try {
        const response = await fetch(
          `/${orgSlug}/organization_users/search?query=${encodeURIComponent(trimmedQuery)}`,
          { signal: controller.signal }
        )
        if (!response.ok) throw new Error('Failed to fetch members')
        const data = (await response.json()) as { members: OrganizationMember[] }
        if (isActive) {
          setMembers(data.members)
        }
      } catch (error) {
        if ((error as Error).name !== 'AbortError') {
          toast.error('Failed to load members')
        }
      } finally {
        if (isActive) {
          setIsLoading(false)
        }
      }
    }, 250)

    return () => {
      isActive = false
      controller.abort()
      window.clearTimeout(timeout)
    }
  }, [orgSlug, query])

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
          <Combobox
            value={assigneeValue}
            inputValue={query}
            onInputValueChange={setQuery}
            onValueChange={(value) => {
              updateAssignee(value)
              if (!value || value === 'unassigned') {
                setQuery('')
                return
              }
              const selected = mergedMembers.find((member) => String(member.id) === value)
              setQuery(selected?.user.name ?? selected?.user.email ?? selectedLabel)
            }}
          >
            <ComboboxInput
              placeholder={selectedLabel}
              showClear
              aria-label="Assign issue"
              onFocus={() => {
                if (!query) {
                  setQuery(selectedLabel === 'Unassigned' ? '' : selectedLabel)
                }
              }}
            />
            <ComboboxContent>
              <ComboboxList>
                <ComboboxItem value="unassigned">Unassigned</ComboboxItem>
                {mergedMembers.map((member) => (
                  <ComboboxItem
                    key={member.id}
                    value={String(member.id)}
                    disabled={Boolean(member.discarded_at)}
                  >
                    {member.user.name}
                    {member.discarded_at ? ' (Archived)' : ''}
                  </ComboboxItem>
                ))}
              </ComboboxList>
              {isLoading && (
                <div className="px-2 py-2 text-xs text-muted-foreground">
                  Loading members...
                </div>
              )}
              {!isLoading && query.trim().length > 0 && mergedMembers.length === 0 && (
                <div className="px-2 py-2 text-xs text-muted-foreground">
                  No members found.
                </div>
              )}
            </ComboboxContent>
          </Combobox>
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
