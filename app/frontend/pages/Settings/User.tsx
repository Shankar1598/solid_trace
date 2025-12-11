import { useForm } from '@inertiajs/react'
import DashboardLayout from '@/components/layouts/DashboardLayout'
import { Card, CardHeader, CardTitle, CardContent, CardDescription, CardFooter } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { User } from '@/types'

interface SettingsUserProps {
  user: User
}

export default function SettingsUser({ user }: SettingsUserProps) {
  const { data, setData, put, processing, errors } = useForm({
    name: user.name,
    email: user.email // Read only typically, but kept in form state for display
  })

  // We need to route this correctly. The controller for UserSettings is singular resource usually
  // Assuming route helper /user/settings
  const submit: React.FormEventHandler = (e) => {
    e.preventDefault()
    put(`/user/settings`)
  }

  return (
    <DashboardLayout>
      <div className="max-w-2xl mx-auto space-y-6">
        <h1 className="text-3xl font-bold tracking-tight">User Settings</h1>

        <Card>
          <form onSubmit={submit}>
            <CardHeader>
              <CardTitle>Profile</CardTitle>
              <CardDescription>
                Manage your personal information.
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="space-y-2">
                <Label htmlFor="email">Email</Label>
                <Input
                  id="email"
                  value={data.email}
                  disabled
                  className="bg-muted"
                />
                <p className="text-xs text-muted-foreground">
                  Email usage is managed by your organization administrator or via support.
                </p>
              </div>

              <div className="space-y-2">
                <Label htmlFor="name">Full Name</Label>
                <Input
                  id="name"
                  value={data.name}
                  onChange={(e) => setData('name', e.target.value)}
                />
                {errors.name && (
                  <p className="text-sm font-medium text-destructive">{errors.name}</p>
                )}
              </div>
            </CardContent>
            <CardFooter className="flex justify-end">
              <Button type="submit" disabled={processing}>
                {processing ? 'Saving...' : 'Save Changes'}
              </Button>
            </CardFooter>
          </form>
        </Card>
      </div>
    </DashboardLayout>
  )
}
