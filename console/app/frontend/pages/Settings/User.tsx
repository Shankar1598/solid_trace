import { useForm, usePage } from '@inertiajs/react'
import SettingsLayout from '@/components/layouts/SettingsLayout'
import { PageHeader } from '@/components/PageHeader'
import { Card, CardHeader, CardTitle, CardContent, CardFooter } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { User } from '@/types'
import { toast } from 'sonner'

interface SettingsUserProps {
  user: User
}

export default function SettingsUser({ user }: SettingsUserProps) {
  const { url } = usePage()
  const { data, setData, put, processing, errors, transform } = useForm({
    name: user.name,
    email: user.email
  })

  transform((data) => ({
    user: data
  }))

  const submit: React.FormEventHandler = (e) => {
    e.preventDefault()
    // Saved where the page was served: User settings live with and without an Organization.
    put(url, {
      onError: () => toast.error('Failed to update profile')
    })
  }

  return (
    <SettingsLayout>
      <PageHeader title="Account" description="Your name as others in the Organization see it." />
      <div className="space-y-6">
        <Card>
          <form onSubmit={submit}>
            <CardHeader>
              <CardTitle>Profile</CardTitle>
            </CardHeader>
            <CardContent className="space-y-6 pb-2">
              <div className="space-y-2">
                <Label htmlFor="email">Email</Label>
                <Input
                  id="email"
                  value={data.email}
                  disabled
                  placeholder="your-email@example.com"
                />
                <p className="text-xs text-muted-foreground">
                  Email usage is managed by your organization administrator or via support.
                </p>
              </div>

              <div className="space-y-2">
                <Label htmlFor="name">Name</Label>
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
            <CardFooter className="justify-end">
              <Button type="submit" disabled={processing}>
                {processing ? 'Saving…' : 'Save changes'}
              </Button>
            </CardFooter>
          </form>
        </Card>
      </div>
    </SettingsLayout>
  )
}
