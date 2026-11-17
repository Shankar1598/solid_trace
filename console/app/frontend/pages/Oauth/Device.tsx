import { useForm } from '@inertiajs/react'
import { FormEventHandler } from 'react'
import AuthLayout from '@/components/layouts/AuthLayout'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

type Props = {
  // Type a code only from a program that you started yourself.
  warning: string
  // A wrong or expired code.
  error: string | null
  // After Approve or Deny: what to do now.
  done: string | null
}

// The device page (Oauth::DevicesController): the user types the code that
// a program with no browser shows. The field always starts empty: no link
// fills it, so a link that someone sends cannot approve their program.
export default function Device({ warning, error, done }: Props) {
  const { data, setData, post, processing } = useForm({ user_code: '' })

  const submit: FormEventHandler = (e) => {
    e.preventDefault()
    post('/device')
  }

  if (done) {
    return (
      <AuthLayout title="Sign in a program" description="Console has your decision.">
        <p role="status" className="text-sm">{done}</p>
      </AuthLayout>
    )
  }

  return (
    <AuthLayout title="Sign in a program" description="Type the code that the program shows.">
      <form onSubmit={submit} className="space-y-4 text-sm">
        <p role="note" className="border border-amber-500/50 bg-amber-500/10 p-3">{warning}</p>
        <div className="space-y-2">
          <Label htmlFor="user_code">Code</Label>
          <Input
            id="user_code"
            value={data.user_code}
            onChange={(e) => setData('user_code', e.target.value)}
            placeholder="XXXX-XXXX"
            autoComplete="off"
            autoCapitalize="characters"
            spellCheck={false}
            autoFocus
            className="font-mono tracking-widest"
            aria-invalid={error ? true : undefined}
            aria-describedby={error ? 'user_code_error' : undefined}
          />
          {error && <p id="user_code_error" role="alert" className="text-destructive">{error}</p>}
        </div>
        <Button type="submit" className="w-full" disabled={processing || data.user_code.trim() === ''}>
          {processing ? 'Checking…' : 'Continue'}
        </Button>
      </form>
    </AuthLayout>
  )
}
