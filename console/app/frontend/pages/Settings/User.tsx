import { Link, useForm, usePage } from '@inertiajs/react'
import SettingsLayout from '@/components/layouts/SettingsLayout'
import { PageHeader } from '@/components/PageHeader'
import { Card, CardHeader, CardTitle, CardContent, CardFooter } from '@/components/ui/card'
import { Button, buttonVariants } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { SecondStepFields, emptySecondStepAnswer, sendWithPasskeyProof, type SecondStepProof } from '@/components/SecondStepFields'
import { PasskeysCard, type PasskeyRow } from '@/components/PasskeysCard'
import { passkeysSupported, type PasskeyOfferHere } from '@/lib/passkeys'
import { User } from '@/types'
import { toast } from 'sonner'

interface SettingsUserProps {
  user: User
  // The Authenticator app once set up, or null.
  authenticator_app: { set_up_at: string } | null
  // How many Recovery codes are unused, or null without Two-factor.
  recovery_codes_left: number | null
  // Where Passkeys are offered and whether here, and the user's Passkeys,
  // those for another host too.
  passkey_offer: PasskeyOfferHere
  passkeys: PasskeyRow[]
}

export default function SettingsUser({ user, authenticator_app, recovery_codes_left, passkey_offer, passkeys }: SettingsUserProps) {
  const { url } = usePage()
  // How the user can prove it's them: a code, a Recovery code (always), or
  // one of their Passkeys for this host, on a page opened there.
  const proof: SecondStepProof = {
    authenticatorApp: !!authenticator_app,
    passkey: passkey_offer.here && passkeysSupported() && passkeys.some((passkey) => !passkey.other_host),
  }
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
        <TwoFactorCard authenticatorApp={authenticator_app} hasPasskeys={passkeys.length > 0} proof={proof} />
        {recovery_codes_left !== null && <RecoveryCodesCard left={recovery_codes_left} proof={proof} />}
        <PasskeysCard offer={passkey_offer} passkeys={passkeys} proof={proof} twoFactor={!!authenticator_app || passkeys.length > 0} />
      </div>
    </SettingsLayout>
  )
}

// Two-factor: set up an Authenticator app on its own page, or remove it with
// a code, a Passkey or a Recovery code (AuthenticatorAppsController). Passkeys
// count as Two-factor too, and have their own card.
function TwoFactorCard({ authenticatorApp, hasPasskeys, proof }: {
  authenticatorApp: SettingsUserProps['authenticator_app']
  hasPasskeys: boolean
  proof: SecondStepProof
}) {
  const { data, setData, delete: destroy, processing } = useForm(emptySecondStepAnswer)

  const remove: React.FormEventHandler = (e) => {
    e.preventDefault()
    destroy('/authenticator_app', { preserveScroll: true, onFinish: () => setData(emptySecondStepAnswer) })
  }

  if (!authenticatorApp) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>Two-factor</CardTitle>
        </CardHeader>
        <CardContent className="pb-2">
          <p className="text-sm text-muted-foreground">
            {hasPasskeys
              ? 'On with your passkeys: after your password, signing in asks for one of them. An authenticator app works too.'
              : 'Off: your password alone signs you in. With an authenticator app or a passkey, signing in also asks for a code from the app, or for the passkey.'}
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
      <form onSubmit={remove} aria-label="Remove authenticator app">
        <CardHeader>
          <CardTitle>Two-factor</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4 pb-2">
          <p className="text-sm text-muted-foreground">
            On: signing in asks for a code from the authenticator app you set up on{' '}
            {new Date(authenticatorApp.set_up_at).toLocaleDateString(undefined, { dateStyle: 'medium' })}.
            To move to a new phone, remove it and set it up again.
            {hasPasskeys ? ' Your passkeys keep Two-factor on.' : ' Removing it turns Two-factor off and throws away your Recovery codes.'}
          </p>
          <div className="max-w-xs">
            <SecondStepFields
              id="remove-code"
              answer={data}
              onChange={setData}
              authenticatorApp={proof.authenticatorApp}
              onPasskey={proof.passkey ? () => sendWithPasskeyProof('delete', '/authenticator_app') : undefined}
            />
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

// Recovery codes: how many are left, and a new set, which replaces the old,
// for a code, a Passkey or a Recovery code (RecoveryCodesController). The new set is
// shown once on its own page.
function RecoveryCodesCard({ left, proof }: { left: number, proof: SecondStepProof }) {
  const { data, setData, post, processing } = useForm(emptySecondStepAnswer)

  const makeNew: React.FormEventHandler = (e) => {
    e.preventDefault()
    post('/recovery_codes', { preserveScroll: true, onFinish: () => setData(emptySecondStepAnswer) })
  }

  return (
    <Card>
      <form onSubmit={makeNew} aria-label="Make new Recovery codes">
        <CardHeader>
          <CardTitle>Recovery codes</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4 pb-2">
          <p className="text-sm text-muted-foreground">
            {left === 0
              ? 'You have no Recovery codes left. Make new ones, so you can still sign in if you lose your phone or passkeys.'
              : `You have ${left} Recovery code${left === 1 ? '' : 's'} left. Each signs you in once if you can't use your authenticator app or passkeys.`}
            {' '}A new set replaces the old one.
          </p>
          <div className="max-w-xs">
            <SecondStepFields
              id="new-codes-code"
              answer={data}
              onChange={setData}
              authenticatorApp={proof.authenticatorApp}
              onPasskey={proof.passkey ? () => sendWithPasskeyProof('post', '/recovery_codes') : undefined}
            />
          </div>
        </CardContent>
        <CardFooter className="justify-end">
          <Button type="submit" variant="outline" disabled={processing}>
            {processing ? 'Making…' : 'Make new Recovery codes'}
          </Button>
        </CardFooter>
      </form>
    </Card>
  )
}
