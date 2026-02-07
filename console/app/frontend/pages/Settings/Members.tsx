import { useForm, usePage, router } from '@inertiajs/react'
import SettingsLayout from '@/components/layouts/SettingsLayout'
import { Card, CardHeader, CardTitle, CardContent, CardDescription } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { SharedProps, Organization } from '@/types'
import { UserPlus, Trash2 } from 'lucide-react'
import { toast } from 'sonner'

interface SettingsMembersProps {
  organization: Organization & {
    members?: Array<{
      id: number
      user: {
        email: string
        name: string
      }
      role: 'owner' | 'member'
    }>
  }
}

export default function SettingsMembers({ organization }: SettingsMembersProps) {
  const { current_user } = usePage<SharedProps>().props

  const { data, setData, post, processing, errors, reset } = useForm({
    email: '',
    name: ''
  })

  const submitMember: React.FormEventHandler = (e) => {
    e.preventDefault()
    post(`/${organization.slug}/settings/members`, {
      onSuccess: () => {
        reset()
        toast.success('Member invited successfully')
      },
      onError: () => toast.error('Failed to invite member'),
      preserveScroll: true
    })
  }

  const removeMember = (id: number) => {
    if (confirm('Are you sure you want to remove this member?')) {
      router.delete(`/${organization.slug}/settings/members/${id}`, {
        onSuccess: () => toast.success('Member removed successfully'),
        onError: () => toast.error('Failed to remove member')
      })
    }
  }

  return (
    <SettingsLayout>
      <div className="space-y-8">
        <div>
          <h1 className="text-3xl font-bold tracking-tight mb-1">Team Members</h1>
          <p className="text-muted-foreground">
            Invite and manage members of your organization.
          </p>
        </div>

        <div className="space-y-6">
          <Card>
            <CardHeader>
              <CardTitle>Team Members</CardTitle>
              <CardDescription>
                Invite colleagues to collaborate on your projects.
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-6">
              {/* Add Member Form */}
              <div className="p-4 border rounded-lg bg-muted/40 space-y-4">
                <h4 className="font-medium text-sm flex items-center gap-2">
                  <UserPlus className="h-4 w-4" /> Invite New Member
                </h4>
                <form onSubmit={submitMember} className="grid sm:grid-cols-2 gap-4 items-end">
                  <div className="space-y-2">
                    <Label htmlFor="newMemberEmail" className="sr-only">Email</Label>
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
                    <Label htmlFor="newMemberName" className="sr-only">Name</Label>
                    <Input
                      id="newMemberName"
                      placeholder="Name (Optional)"
                      value={data.name}
                      onChange={(e) => setData('name', e.target.value)}
                    />
                    {errors.name && <p className="text-xs text-destructive">{errors.name}</p>}
                  </div>
                  <Button type="submit" disabled={processing} size="sm">
                    {processing ? 'Inviting...' : 'Invite'}
                  </Button>
                </form>
              </div>

              {/* Members List */}
              <div className="divide-y border rounded-md">
                {organization.members?.map((member) => (
                  <div key={member.id} className="flex items-center justify-between p-4 bg-card">
                    <div className="flex items-center gap-3">
                      <div className="h-9 w-9 rounded-full bg-primary/10 flex items-center justify-center text-primary text-xs font-bold">
                        {member.user.name.charAt(0)}
                      </div>
                      <div>
                        <p className="text-sm font-medium">{member.user.name}</p>
                        <p className="text-xs text-muted-foreground">{member.user.email}</p>
                      </div>
                    </div>
                    <div className="flex items-center gap-4">
                      <span className="text-[10px] font-bold text-muted-foreground uppercase border px-2 py-0.5 rounded tracking-wider">
                        {member.role}
                      </span>

                      {current_user?.email !== member.user.email && (
                        <Button
                          variant="ghost"
                          size="icon"
                          className="h-8 w-8 text-muted-foreground hover:text-destructive"
                          onClick={() => removeMember(member.id)}
                        >
                          <Trash2 className="h-4 w-4" />
                        </Button>
                      )}
                    </div>
                  </div>
                ))}
              </div>
            </CardContent>
          </Card>
        </div>
      </div>
    </SettingsLayout>
  )
}
