import { Link, router, useForm } from '@inertiajs/react'
import { FormEventHandler } from 'react'
import AuthLayout from '@/components/layouts/AuthLayout'
import { Button } from '@/components/ui/button'
import { SecondStepFields, emptySecondStepAnswer, passkeyAnswer } from '@/components/SecondStepFields'
import { passkeysSupported } from '@/lib/passkeys'

interface SecondStepProps {
  // Whether the user has an Authenticator app.
  authenticator_app: boolean
  // Whether one of the user's Passkeys can answer here (the Console URL).
  passkey: boolean
}

// Signing in's second step, after a right password from a user with
// Two-factor (SecondStepsController): a code from the app, one of their
// Passkeys, or a Recovery code. It lasts five minutes; after that, or with
// Start over, the user signs in again.
export default function SecondStep({ authenticator_app, passkey }: SecondStepProps) {
  const { data, setData, post, processing } = useForm(emptySecondStepAnswer)

  const submit: FormEventHandler = (e) => {
    e.preventDefault()
    post('/login/second_step', { onFinish: () => setData(emptySecondStepAnswer) })
  }

  const answerWithPasskey = async () => {
    const credential = await passkeyAnswer('/login/second_step/passkey_options')
    if (credential) router.post('/login/second_step', { credential })
  }

  const ways = [
    authenticator_app && 'enter the code your authenticator app shows for SolidTrace',
    passkey && 'use one of your passkeys',
  ].filter(Boolean)
  const description = [...ways, `${ways.length ? 'or ' : ''}give one of your Recovery codes.`]
    .join(', ').replace(/^./, (first) => first.toUpperCase())

  return (
    <AuthLayout title="Two-factor" description={description}>
      <form onSubmit={submit} className="space-y-4">
        <SecondStepFields
          answer={data}
          onChange={setData}
          autoFocus
          authenticatorApp={authenticator_app}
          onPasskey={passkey && passkeysSupported() ? answerWithPasskey : undefined}
        />

        <Button type="submit" className="w-full" disabled={processing}>
          {processing ? 'Checking…' : 'Continue'}
        </Button>

        <div className="text-center text-sm">
          <Link href="/login" className="text-muted-foreground underline-offset-2 hover:underline">
            Start over
          </Link>
        </div>
      </form>
    </AuthLayout>
  )
}
