import { Link, useForm } from '@inertiajs/react'
import { FormEventHandler } from 'react'
import AuthLayout from '@/components/layouts/AuthLayout'
import { Button } from '@/components/ui/button'
import { SecondStepFields, emptySecondStepAnswer } from '@/components/SecondStepFields'

// Signing in's second step, after a right password from a user with
// Two-factor (SecondStepsController): a code from the app or a Recovery code. It lasts five minutes; after that, or
// with Start over, the user signs in again.
export default function SecondStep() {
  const { data, setData, post, processing } = useForm(emptySecondStepAnswer)

  const submit: FormEventHandler = (e) => {
    e.preventDefault()
    post('/login/second_step', { onFinish: () => setData(emptySecondStepAnswer) })
  }

  return (
    <AuthLayout title="Two-factor" description="Enter the code your authenticator app shows for SolidTrace, or one of your Recovery codes.">
      <form onSubmit={submit} className="space-y-4">
        <SecondStepFields answer={data} onChange={setData} autoFocus />

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
