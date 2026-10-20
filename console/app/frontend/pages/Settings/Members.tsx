import { useState } from 'react'
import { useForm, usePage, router } from '@inertiajs/react'
import SettingsLayout from '@/components/layouts/SettingsLayout'
import { PageHeader } from '@/components/PageHeader'
import { Badge } from '@/components/ui/badge'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { SharedProps, Organization } from '@/types'
import { copyToClipboard } from '@/lib/clipboard'
import { UserPlus, Trash2, Link as LinkIcon } from 'lucide-react'
import { toast } from 'sonner'

const ROLES = { admin: 'Admin', member: 'Member' }

interface SettingsMembersProps {
  organization: Organization & {
    members?: Array<{
      id: number
      user: {
        email: string
        name: string
      }
      role: 'admin' | 'member'
      invited: boolean
      // Only admins get it, and only on Invited rows.
      invite_url?: string
    }>
  }
  can_manage_members: boolean
}

export default function SettingsMembers({ organization, can_manage_members }: SettingsMembersProps) {
  const { current_user } = usePage<SharedProps>().props
  // The invite link to show, when the browser wouldn't copy it.
  const [shownLink, setShownLink] = useState<string | null>(null)

  const { data, setData, post, processing, errors, reset, transform } = useForm({
    email: '',
    name: ''
  })

  const submitMember: React.FormEventHandler = (e) => {
    e.preventDefault()
    // The controller reads params[:member].
    transform((data) => ({ member: data }))
    post(`/${organization.slug}/settings/organization_users`, {
      onSuccess: () => {
        reset()
      },
      preserveScroll: true
    })
  }

  const removeMember = (id: number, invited: boolean) => {
    const question = invited ? 'Are you sure you want to revoke this invite?' : 'Are you sure you want to remove this member?'
    if (confirm(question)) {
      router.delete(`/${organization.slug}/settings/organization_users/${id}`, {
        onError: () => toast.error(invited ? 'Failed to revoke invite' : 'Failed to remove member')
      })
    }
  }

  const copyInviteLink = async (url: string) => {
    if (await copyToClipboard(url)) {
      toast.success('Invite link copied')
    } else {
      setShownLink(url)
    }
  }

  const updateRole = (id: number, role: string | null) => {
    router.patch(`/${organization.slug}/settings/organization_users/${id}`, {
      organization_user: { role }
    }, {
      onError: (errors) => {
        const firstError = Object.values(errors)[0]
        const message = Array.isArray(firstError) ? firstError[0] : firstError
        toast.error(message || 'Failed to update member role')
      },
      preserveScroll: true
    })
  }

  return (
    <SettingsLayout>
      <PageHeader title="Members" description="People in the Organization, and who can change its settings." />
      <div className="space-y-6">
        {can_manage_members && (
          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2">
                <UserPlus className="h-4 w-4" /> Invite a member
              </CardTitle>
            </CardHeader>
            <CardContent>
              <form onSubmit={submitMember} className="grid sm:grid-cols-2 lg:grid-cols-3 gap-4 items-end">
                <div className="space-y-2">
                  <Label htmlFor="newMemberEmail">Email</Label>
                  <Input
                    id="newMemberEmail"
                    placeholder="colleague@example.com"
                    type="email"
                    value={data.email}
                    onChange={(e) => setData('email', e.target.value)}
                    required
                  />
                  {errors.email && <p className="text-xs text-destructive">{errors.email}</p>}
                </div>
                <div className="space-y-2">
                  <Label htmlFor="newMemberName">Name (optional)</Label>
                  <Input
                    id="newMemberName"
                    placeholder="e.g. John Doe"
                    value={data.name}
                    onChange={(e) => setData('name', e.target.value)}
                  />
                  {errors.name && <p className="text-xs text-destructive">{errors.name}</p>}
                </div>
                <Button type="submit" disabled={processing} className="w-full sm:w-auto sm:justify-self-start">
                  {processing ? 'Inviting…' : 'Invite'}
                </Button>
              </form>
            </CardContent>
          </Card>
        )}

        <div className="border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Member</TableHead>
                <TableHead>Role</TableHead>
                <TableHead><span className="sr-only">Actions</span></TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {organization.members?.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={3} className="p-8 text-center text-muted-foreground">No members found.</TableCell>
                </TableRow>
              ) : (
                organization.members?.map((member) => (
                  <TableRow key={member.id}>
                    <TableCell>
                      <div className="flex items-center gap-3">
                        <div className="h-8 w-8 shrink-0 rounded-full bg-muted border flex items-center justify-center text-foreground text-xs font-semibold">
                          {member.user.name.charAt(0).toUpperCase()}
                        </div>
                        <div>
                          <p className="font-medium flex items-center gap-2">
                            {member.user.name}
                            {member.invited && <Badge variant="info">Invited</Badge>}
                          </p>
                          <p className="text-xs text-muted-foreground">{member.user.email}</p>
                        </div>
                      </div>
                    </TableCell>
                    <TableCell>
                      {can_manage_members ? (
                        <Select value={member.role} onValueChange={(val) => updateRole(member.id, val)} items={ROLES}>
                          <SelectTrigger size="sm" className="w-28" aria-label={`Role of ${member.user.name}`}>
                            <SelectValue />
                          </SelectTrigger>
                          <SelectContent>
                            <SelectItem value="member">Member</SelectItem>
                            <SelectItem value="admin">Admin</SelectItem>
                          </SelectContent>
                        </Select>
                      ) : (
                        <Badge>{ROLES[member.role]}</Badge>
                      )}
                    </TableCell>
                    <TableCell className="text-right">
                      {member.invite_url && (
                        <Button
                          variant="ghost"
                          size="icon"
                          aria-label={`Copy invite link for ${member.user.name}`}
                          className="text-muted-foreground hover:text-foreground"
                          onClick={() => copyInviteLink(member.invite_url!)}
                        >
                          <LinkIcon className="h-4 w-4" />
                        </Button>
                      )}
                      {can_manage_members && current_user?.email !== member.user.email && (
                        <Button
                          variant="ghost"
                          size="icon"
                          aria-label={member.invited ? `Revoke invite for ${member.user.name}` : `Remove ${member.user.name}`}
                          className="text-muted-foreground hover:text-destructive"
                          onClick={() => removeMember(member.id, member.invited)}
                        >
                          <Trash2 className="h-4 w-4" />
                        </Button>
                      )}
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </div>
      </div>

      <Dialog open={shownLink !== null} onOpenChange={(open) => { if (!open) setShownLink(null) }}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Invite link</DialogTitle>
            <DialogDescription>This browser wouldn't copy it. Copy the link below and send it to them.</DialogDescription>
          </DialogHeader>
          <Input
            aria-label="Invite link"
            readOnly
            autoFocus
            value={shownLink ?? ''}
            onFocus={(e) => e.currentTarget.select()}
          />
        </DialogContent>
      </Dialog>
    </SettingsLayout>
  )
}
