import { Link, useForm } from '@inertiajs/react'
import { FormEventHandler } from 'react'
import AuthLayout from '@/components/layouts/AuthLayout'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

// Where a user in no Organization makes one, and becomes its admin.
export default function NewOrganization() {
  const { data, setData, post, processing, errors, transform } = useForm({
    name: '',
  })

  transform((data) => ({ organization: data }))

  const submit: FormEventHandler = (e) => {
    e.preventDefault()
    post('/organizations')
  }

  return (
    <AuthLayout title="Make an Organization" description="An Organization holds your Projects, Dashboards and Monitors, and the people who share them.">
      <form onSubmit={submit} className="space-y-4">
        <div className="space-y-2">
          <Label htmlFor="name">Name</Label>
          <Input
            id="name"
            placeholder="Acme"
            value={data.name}
            onChange={(e) => setData('name', e.target.value)}
            required
          />
          {errors.name && (
            <p className="text-sm font-medium text-destructive">{errors.name}</p>
          )}
        </div>

        <Button type="submit" className="w-full" disabled={processing}>
          {processing ? 'Making…' : 'Make Organization'}
        </Button>

        <p className="text-center text-sm text-muted-foreground">
          Joining someone else's? Ask one of its admins to invite you.
        </p>

        <div className="text-center text-sm">
          <Link href="/session" method="delete" as="button" className="underline">
            Log out
          </Link>
        </div>
      </form>
    </AuthLayout>
  )
}
