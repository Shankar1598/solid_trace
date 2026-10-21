import { Link, useForm } from '@inertiajs/react'
import { FormEventHandler } from 'react'
import AuthLayout from '@/components/layouts/AuthLayout'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

// Sign-up is offered only while RegistrationsController allows it: on an
// install with users, once the operator has opened it.
export default function Login({ sign_up_allowed }: { sign_up_allowed: boolean }) {
  const { data, setData, post, processing, errors } = useForm({
    email: '',
    password: '',
  })

  const submit: FormEventHandler = (e) => {
    e.preventDefault()
    post('/login')
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

        <Button type="submit" className="w-full" disabled={processing}>
          {processing ? 'Logging in…' : 'Log in'}
        </Button>

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
