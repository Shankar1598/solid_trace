import { Link, useForm } from '@inertiajs/react'
import { FormEventHandler } from 'react'
import SettingsLayout from '@/components/layouts/SettingsLayout'
import { PageHeader } from '@/components/PageHeader'
import { Card, CardContent, CardFooter, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

interface TwoFactorSetUpProps {
  // The Authenticator app's secret, while it's being set up.
  secret: string
  // The otpauth:// URI the QR code holds, which opens an app on a phone.
  uri: string
  // The QR code, an SVG drawn by Console.
  qr_code: string
  // The Organization to return to once it's set up, from ?org=;
  // requires_two_factor when its Require two-factor sent the user here.
  return_organization: { name: string; slug: string; requires_two_factor: boolean } | null
}

// The Authenticator app's set-up page (AuthenticatorAppsController), outside
// any Organization: scan the QR code or type the secret, then confirm with a
// code. Until a code confirms it, it isn't Two-factor.
export default function TwoFactorSetUp({ secret, uri, qr_code, return_organization }: TwoFactorSetUpProps) {
  const { data, setData, post, processing } = useForm({ code: '', org: return_organization?.slug ?? '' })

  const submit: FormEventHandler = (e) => {
    e.preventDefault()
    post('/authenticator_app', { onFinish: () => setData('code', '') })
  }

  return (
    <SettingsLayout>
      <PageHeader
        title="Set up Two-factor"
        description="Once it's on, signing in asks for a code from your authenticator app after your password."
      />
      {return_organization?.requires_two_factor && (
        <p role="status" className="mb-4 max-w-xl rounded-md border px-4 py-3 text-sm">
          {return_organization.name} requires Two-factor. Set it up to open {return_organization.name}.
        </p>
      )}
      <Card className="max-w-xl">
        <form onSubmit={submit}>
          <CardHeader>
            <CardTitle>Authenticator app</CardTitle>
          </CardHeader>
          <CardContent className="space-y-6">
            <div className="space-y-3">
              <p className="text-sm">
                1. Scan this QR code with an authenticator app, such as 1Password, Google Authenticator or Authy.
              </p>
              {/* Dark on light in both themes, so every camera reads it. */}
              <div
                className="w-48 bg-white p-3 [&_svg]:h-auto [&_svg]:w-full"
                role="img"
                aria-label="QR code for your authenticator app"
                dangerouslySetInnerHTML={{ __html: qr_code }}
              />
              <p className="text-sm text-muted-foreground">
                Can't scan it? Enter this key instead:{' '}
                <code aria-label="Secret key" className="whitespace-nowrap font-mono text-foreground">
                  {secret.match(/.{1,4}/g)?.join(' ')}
                </code>
                {' '}or <a href={uri} className="underline underline-offset-2">open it in an app on this device</a>.
              </p>
            </div>

            <div className="space-y-2">
              <Label htmlFor="code">2. Enter the code the app shows</Label>
              <Input
                id="code"
                inputMode="numeric"
                autoComplete="one-time-code"
                placeholder="123456"
                maxLength={7}
                className="max-w-40"
                value={data.code}
                onChange={(e) => setData('code', e.target.value)}
                required
              />
            </div>
          </CardContent>
          <CardFooter className="justify-between">
            <Link href="/user_settings" className="text-sm text-muted-foreground underline-offset-2 hover:underline">
              Cancel
            </Link>
            <Button type="submit" disabled={processing}>
              {processing ? 'Checking…' : 'Turn on Two-factor'}
            </Button>
          </CardFooter>
        </form>
      </Card>
      {return_organization && (
        <p className="mt-4 text-sm text-muted-foreground">
          Once it's on, you'll go back to {return_organization.name}.
        </p>
      )}
      {/* Pages outside an Organization have no sidebar, so no other way out. */}
      <p className="mt-4 text-sm text-muted-foreground">
        Not now?{' '}
        <Link href="/session" method="delete" as="button" className="underline underline-offset-2 hover:text-foreground">
          Sign out
        </Link>
      </p>
    </SettingsLayout>
  )
}
