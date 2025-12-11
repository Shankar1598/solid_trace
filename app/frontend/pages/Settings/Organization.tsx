import { useForm, usePage } from '@inertiajs/react'
import DashboardLayout from '@/components/layouts/DashboardLayout'
import { Card, CardHeader, CardTitle, CardContent, CardDescription, CardFooter } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { SharedProps, Organization } from '@/types'
import { UserPlus, Trash2 } from 'lucide-react'
import { router } from '@inertiajs/react'

interface SettingsOrganizationProps {
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

export default function SettingsOrganization({ organization }: SettingsOrganizationProps) {
  const { current_user } = usePage<SharedProps>().props

  const { data, setData, put, processing, errors } = useForm({
    name: organization.name
  })

  // Form for adding new member
  const { data: memberData, setData: setMemberData, post: postMember, processing: processingMember, errors: memberErrors, reset: resetMember } = useForm({
    email: '',
    name: ''
  })

  const submitOrg: React.FormEventHandler = (e) => {
    e.preventDefault()
    put(`/${organization.slug}/settings`, {
      preserveScroll: true
    })
  }

  const submitMember: React.FormEventHandler = (e) => {
    e.preventDefault()
    postMember(`/${organization.slug}/settings/members`, {
      onSuccess: () => resetMember(),
      preserveScroll: true
    })
  }

  const removeMember = (id: number) => {
    if (confirm('Are you sure you want to remove this member?')) {
      router.delete(`/${organization.slug}/settings/members/${id}`)
    }
  }

  return (
    <DashboardLayout>
      <div className="max-w-4xl mx-auto space-y-6">
        <h1 className="text-3xl font-bold tracking-tight">Organization Settings</h1>

        <Tabs defaultValue="general">
          <TabsList>
            <TabsTrigger value="general">General</TabsTrigger>
            <TabsTrigger value="members">Members</TabsTrigger>
          </TabsList>

          <TabsContent value="general" className="space-y-6">
            <Card>
              <form onSubmit={submitOrg}>
                <CardHeader>
                  <CardTitle>General Settings</CardTitle>
                  <CardDescription>
                    Manage your organization profile.
                  </CardDescription>
                </CardHeader>
                <CardContent className="space-y-4">
                  <div className="space-y-2">
                    <Label htmlFor="orgName">Organization Name</Label>
                    <Input
                      id="orgName"
                      value={data.name}
                      onChange={(e) => setData('name', e.target.value)}
                    />
                    {errors.name && (
                      <p className="text-sm font-medium text-destructive">{errors.name}</p>
                    )}
                  </div>
                  <div className="space-y-2">
                    <Label>Slug</Label>
                    <Input value={organization.slug} disabled className="font-mono bg-muted" />
                    <p className="text-xs text-muted-foreground">
                      The slug is used in URLs and cannot be changed.
                    </p>
                  </div>
                </CardContent>
                <CardFooter className="flex justify-end">
                  <Button type="submit" disabled={processing}>
                    {processing ? 'Saving...' : 'Save Changes'}
                  </Button>
                </CardFooter>
              </form>
            </Card>
          </TabsContent>

          <TabsContent value="members" className="space-y-6">
            <Card>
              <CardHeader>
                <CardTitle>Team Members</CardTitle>
                <CardDescription>
                  Invite and manage members of your organization.
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
                        value={memberData.email}
                        onChange={(e) => setMemberData('email', e.target.value)}
                        required
                      />
                      {memberErrors.email && <p className="text-xs text-destructive">{memberErrors.email}</p>}
                    </div>
                    <div className="space-y-2">
                      <Label htmlFor="newMemberName" className="sr-only">Name</Label>
                      <Input
                        id="newMemberName"
                        placeholder="Name (Optional)"
                        value={memberData.name}
                        onChange={(e) => setMemberData('name', e.target.value)}
                      />
                      {memberErrors.name && <p className="text-xs text-destructive">{memberErrors.name}</p>}
                    </div>
                    <Button type="submit" disabled={processingMember} size="sm">
                      {processingMember ? 'Inviting...' : 'Invite'}
                    </Button>
                  </form>
                </div>

                {/* Members List */}
                <div className="space-y-4">
                  {organization.members?.map((member) => (
                    <div key={member.id} className="flex items-center justify-between p-2 border-b last:border-0">
                      <div>
                        <p className="font-medium">{member.user.name}</p>
                        <p className="text-sm text-muted-foreground">{member.user.email}</p>
                      </div>
                      <div className="flex items-center gap-4">
                        <span className="text-sm font-mono bg-muted px-2 py-1 rounded capitalize">
                          {member.role}
                        </span>

                        {/* Can't remove yourself */}
                        {current_user?.email !== member.user.email && (
                          <Button
                            variant="ghost"
                            size="icon"
                            className="text-muted-foreground hover:text-destructive"
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
          </TabsContent>
        </Tabs>
      </div>
    </DashboardLayout>
  )
}
