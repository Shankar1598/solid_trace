import { Link, useForm, usePage } from '@inertiajs/react'
import SettingsLayout from '@/components/layouts/SettingsLayout'
import { PageHeader } from '@/components/PageHeader'
import { Card, CardHeader, CardTitle, CardContent, CardFooter } from '@/components/ui/card'
import { Button, buttonVariants } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { SecondStepFields, emptySecondStepAnswer } from '@/components/SecondStepFields'
import { User } from '@/types'
import { toast } from 'sonner'

interface SettingsUserProps {
  user: User
  // The Authenticator app once set up, or null.
  authenticator_app: { set_up_at: string } | null
}

export default function SettingsUser({ user, authenticator_app }: SettingsUserProps) {
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
        <TwoFactorCard authenticatorApp={authenticator_app} />
      </div>
    </SettingsLayout>
  )
}

// Two-factor: set up an Authenticator app on its own page, or remove it with
// a code from it (AuthenticatorAppsController).
function TwoFactorCard({ authenticatorApp }: { authenticatorApp: SettingsUserProps['authenticator_app'] }) {
  const { data, setData, delete: destroy, processing } = useForm(emptySecondStepAnswer)

  const remove: React.FormEventHandler = (e) => {
    e.preventDefault()
    destroy('/authenticator_app', { preserveScroll: true, onFinish: () => setData('code', '') })
  }

  if (!authenticatorApp) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>Two-factor</CardTitle>
        </CardHeader>
        <CardContent className="pb-2">
          <p className="text-sm text-muted-foreground">
            Off: your password alone signs you in. With an authenticator app, signing in also asks for a code from it.
          </p>
        </CardContent>
        <CardFooter className="justify-end">
          <Link href="/authenticator_app/new" className={buttonVariants()}>Set up an authenticator app</Link>
        </CardFooter>
      </Card>
    )
  }

  return (
    <Card>
      <form onSubmit={remove}>
        <CardHeader>
          <CardTitle>Two-factor</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4 pb-2">
          <p className="text-sm text-muted-foreground">
            On: signing in asks for a code from the authenticator app you set up on{' '}
            {new Date(authenticatorApp.set_up_at).toLocaleDateString(undefined, { dateStyle: 'medium' })}.
            To move to a new phone, remove it and set it up again.
          </p>
          <div className="max-w-xs">
            <SecondStepFields id="remove-code" answer={data} onChange={setData} />
          </div>
        </CardContent>
        <CardFooter className="justify-end">
          <Button type="submit" variant="destructive" disabled={processing}>
            {processing ? 'Removing…' : 'Remove authenticator app'}
          </Button>
        </CardFooter>
      </form>
    </Card>
  )
}
