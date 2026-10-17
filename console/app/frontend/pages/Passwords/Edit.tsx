import { useForm } from '@inertiajs/react'
import { FormEventHandler } from 'react'
import AuthLayout from '@/components/layouts/AuthLayout'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'

interface EditPasswordProps {
  // The reset link's token, which the new password is saved under.
  token: string
}

// The page a reset link opens: a new password, entered twice.
export default function EditPassword({ token }: EditPasswordProps) {
  const { data, setData, put, processing, errors } = useForm({
    password: '',
    password_confirmation: '',
  })

  const submit: FormEventHandler = (e) => {
    e.preventDefault()
    put(`/passwords/${encodeURIComponent(token)}`)
  }

  return (
    <AuthLayout>
      <Card>
        <CardHeader>
          <CardTitle>Choose a new password</CardTitle>
          <CardDescription>
            Saving it signs you out everywhere
          </CardDescription>
        </CardHeader>
        <CardContent>
          <form onSubmit={submit} className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="password">New password</Label>
              <Input
                id="password"
                type="password"
                autoComplete="new-password"
                value={data.password}
                onChange={(e) => setData('password', e.target.value)}
                required
              />
              {errors.password && (
                <p className="text-sm font-medium text-destructive">{errors.password}</p>
              )}
            </div>

            <div className="space-y-2">
              <Label htmlFor="password_confirmation">Confirm new password</Label>
              <Input
                id="password_confirmation"
                type="password"
                autoComplete="new-password"
                value={data.password_confirmation}
                onChange={(e) => setData('password_confirmation', e.target.value)}
                required
              />
              {errors.password_confirmation && (
                <p className="text-sm font-medium text-destructive">{errors.password_confirmation}</p>
              )}
            </div>

            <Button type="submit" className="w-full" disabled={processing}>
              {processing ? 'Saving...' : 'Save password'}
            </Button>
          </form>
        </CardContent>
      </Card>
    </AuthLayout>
  )
}
