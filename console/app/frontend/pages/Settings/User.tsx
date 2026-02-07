import { useForm } from '@inertiajs/react'
import SettingsLayout from '@/components/layouts/SettingsLayout'
import { Card, CardHeader, CardTitle, CardContent, CardDescription, CardFooter } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { User } from '@/types'
import { toast } from 'sonner'

interface SettingsUserProps {
  user: User
}

export default function SettingsUser({ user }: SettingsUserProps) {
  const { data, setData, put, processing, errors } = useForm({
    name: user.name,
    email: user.email
  })

  const submit: React.FormEventHandler = (e) => {
    e.preventDefault()
    put(`/user/settings`, {
      onError: () => toast.error('Failed to update profile')
    })
  }

  return (
    <SettingsLayout>
      <div className="space-y-8">
        <div>
          <h1 className="text-3xl font-bold tracking-tight mb-1">Account Settings</h1>
          <p className="text-muted-foreground">
            Manage your personal profile and account preferences.
          </p>
        </div>

        <Card>
          <form onSubmit={submit}>
            <CardHeader>
              <CardTitle>Profile</CardTitle>
              <CardDescription>
                Manage your personal information.
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-6 pb-6">
              <div className="space-y-2">
                <Label htmlFor="email">Email Address</Label>
                <Input
                  id="email"
                  value={data.email}
                  disabled
                  className="bg-muted"
                  placeholder="your-email@example.com"
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
                  placeholder="Enter your full name"
                />
                {errors.name && (
                  <p className="text-sm font-medium text-destructive">{errors.name}</p>
                )}
              </div>
            </CardContent>
            <CardFooter className="flex justify-end border-t bg-muted/20 px-6 py-4">
              <Button type="submit" disabled={processing}>
                {processing ? 'Saving...' : 'Save Changes'}
              </Button>
            </CardFooter>
          </form>
        </Card>
      </div>
    </SettingsLayout>
  )
}
