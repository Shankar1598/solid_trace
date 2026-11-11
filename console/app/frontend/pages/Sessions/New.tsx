import { Link, router, useForm } from '@inertiajs/react'
import { FormEventHandler, useEffect, useRef, useState } from 'react'
import { KeyRound } from 'lucide-react'
import AuthLayout from '@/components/layouts/AuthLayout'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  getPasskey,
  passkeyAutofillAvailable,
  passkeyCancelled,
  passkeyOptions,
  passkeysSupported,
  type PasskeyOfferHere,
} from '@/lib/passkeys'

// Sign-up is offered only while RegistrationsController allows it: on an
// install with users, once the operator has opened it. The Session lasts 24
// hours, or 7 days with Remember me, which is unticked so that a shared
// computer gets the shorter one.
//
// Where Passkeys work here, "Sign in with a passkey" and the email field's
// autofill sign in with one alone, with the same Remember me
// (PasskeySignInsController); opened at another address, the page says
// where they work.
export default function Login({ sign_up_allowed, passkey_offer }: { sign_up_allowed: boolean, passkey_offer: PasskeyOfferHere }) {
  const { data, setData, post, processing, errors } = useForm({
    email: '',
    password: '',
    remember_me: false,
  })

  const submit: FormEventHandler = (e) => {
    e.preventDefault()
    post('/login')
  }

  const passkeysHere = passkey_offer.here && passkeysSupported()
  const [passkeyBusy, setPasskeyBusy] = useState(false)
  const [passkeyError, setPasskeyError] = useState<string | null>(null)
  // Read when the Passkey answers, which may be long after the page loaded.
  const rememberMe = useRef(data.remember_me)
  rememberMe.current = data.remember_me
  const autofill = useRef<AbortController | null>(null)

  const signInWith = (credential: PublicKeyCredentialJSON) => {
    router.post('/login/passkey', { credential, remember_me: rememberMe.current })
  }

  const failed = (error: unknown) => {
    if (!passkeyCancelled(error)) setPasskeyError(error instanceof Error ? error.message : "That Passkey didn't work.")
  }

  // The email field's autofill offers the user's Passkeys, where the browser can.
  useEffect(() => {
    if (!passkeysHere) return
    const controller = new AbortController()
    autofill.current = controller
    ;(async () => {
      if (!(await passkeyAutofillAvailable()) || controller.signal.aborted) return
      try {
        const options = await passkeyOptions<PublicKeyCredentialRequestOptionsJSON>('/login/passkey/options')
        signInWith(await getPasskey(options, { mediation: 'conditional', signal: controller.signal }))
      } catch (error) {
        if (!controller.signal.aborted) failed(error)
      }
    })()
    return () => controller.abort()
  }, [passkeysHere])

  const signInWithPasskey = async () => {
    autofill.current?.abort()
    setPasskeyError(null)
    setPasskeyBusy(true)
    try {
      const options = await passkeyOptions<PublicKeyCredentialRequestOptionsJSON>('/login/passkey/options')
      signInWith(await getPasskey(options))
    } catch (error) {
      failed(error)
    } finally {
      setPasskeyBusy(false)
    }
  }

  return (
    <AuthLayout title="Log in" description="Enter your email and password to log in.">
      <form onSubmit={submit} className="space-y-4">
        <div className="space-y-2">
          <Label htmlFor="email">Email</Label>
          <Input
            id="email"
            type="email"
            placeholder="m@example.com"
            autoComplete={passkeysHere ? 'username webauthn' : 'username'}
            value={data.email}
            onChange={(e) => setData('email', e.target.value)}
            required
          />
          {errors.email && (
            <p className="text-sm font-medium text-destructive">{errors.email}</p>
          )}
        </div>

        <div className="space-y-2">
          <div className="flex items-center justify-between">
            <Label htmlFor="password">Password</Label>
            <Link
              href="/passwords/new"
              className="text-xs text-muted-foreground underline-offset-2 hover:underline"
            >
              Forgot your password?
            </Link>
          </div>
          <Input
            id="password"
            type="password"
            value={data.password}
            onChange={(e) => setData('password', e.target.value)}
            required
          />
          {errors.password && (
            <p className="text-sm font-medium text-destructive">{errors.password}</p>
          )}
        </div>

        <label className="flex items-center gap-2 text-sm">
          <Checkbox
            aria-label="Remember me"
            checked={data.remember_me}
            onCheckedChange={(checked) => setData('remember_me', checked === true)}
          />
          Remember me
        </label>

        <Button type="submit" className="w-full" disabled={processing}>
          {processing ? 'Logging in…' : 'Log in'}
        </Button>

        {passkeysHere && (
          <Button type="button" variant="outline" className="w-full" onClick={signInWithPasskey} disabled={passkeyBusy}>
            <KeyRound className="h-4 w-4" />
            Sign in with a Passkey
          </Button>
        )}
        {passkeyError && <p className="text-sm font-medium text-destructive" role="alert">{passkeyError}</p>}
        {passkey_offer.offered && !passkey_offer.here && (
          <p className="text-center text-xs text-muted-foreground">
            Passkeys work at <a href={`${passkey_offer.url}/login`} className="underline">{passkey_offer.url}</a>.
          </p>
        )}

        {sign_up_allowed && (
          <div className="mt-4 text-center text-sm">
            Don't have an account?{' '}
            <Link href="/registration/new" className="underline">
              Sign up
            </Link>
          </div>
        )}
      </form>
    </AuthLayout>
  )
}
