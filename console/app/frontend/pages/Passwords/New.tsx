import { Link, useForm } from '@inertiajs/react'
import { FormEventHandler } from 'react'
import AuthLayout from '@/components/layouts/AuthLayout'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'

// "Forgot your password?": the email a reset link is sent to.
export default function ForgotPassword() {
  const { data, setData, post, processing } = useForm({
    email: '',
  })

  const submit: FormEventHandler = (e) => {
    e.preventDefault()
    post('/passwords')
  }

  return (
    <AuthLayout>
      <Card>
        <CardHeader>
          <CardTitle>Reset your password</CardTitle>
          <CardDescription>
            Enter your email, and we'll send you a link to choose a new password
          </CardDescription>
        </CardHeader>
        <CardContent>
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
            </div>

            <Button type="submit" className="w-full" disabled={processing}>
              {processing ? 'Sending...' : 'Send reset link'}
            </Button>

            <div className="mt-4 text-center text-sm">
              Remembered it?{' '}
              <Link href="/login" className="underline">
                Login
              </Link>
            </div>
          </form>
        </CardContent>
      </Card>
    </AuthLayout>
  )
}
