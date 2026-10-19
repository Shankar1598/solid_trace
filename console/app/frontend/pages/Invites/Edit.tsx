import { Link, useForm } from '@inertiajs/react'
import { FormEventHandler } from 'react'
import AuthLayout from '@/components/layouts/AuthLayout'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

interface Invite {
  email: string
  name: string
  organization_name: string
  // The email of someone else signed in on this browser, whom saving signs out.
  signed_in_as: string | null
}

type InviteProps =
  | { token: string; org: string; invite: Invite; expired?: undefined }
  | { expired: string; invite?: undefined }

// The page an Invite link opens: the Invited member's name and a password,
// which sign them in to the Organization that invited them. A link that has
// expired, was used or was revoked says so instead.
export default function EditInvite(props: InviteProps) {
  if (props.invite === undefined) {
    return (
      <AuthLayout title="Invite link expired" description={props.expired}>
        <div className="text-center text-sm">
          <Link href="/login" className="underline">
            Sign in
          </Link>
        </div>
      </AuthLayout>
    )
  }

  return <InviteForm token={props.token} org={props.org} invite={props.invite} />
}

function InviteForm({ token, org, invite }: { token: string; org: string; invite: Invite }) {
  const { data, setData, put, processing, errors } = useForm({
    name: invite.name,
    password: '',
    password_confirmation: '',
  })

  const submit: FormEventHandler = (e) => {
    e.preventDefault()
    put(`/invites/${encodeURIComponent(token)}?org=${encodeURIComponent(org)}`)
  }

  return (
    <AuthLayout
      title={`Join ${invite.organization_name}`}
      description="Choose a password to sign in."
    >
      <form onSubmit={submit} className="space-y-4">
        {invite.signed_in_as && (
          <p className="text-sm text-muted-foreground">
            You're signed in as {invite.signed_in_as}. Setting this password signs you in as {invite.email} instead.
          </p>
        )}

        <div className="space-y-2">
          <Label htmlFor="email">Email</Label>
          <Input id="email" type="email" value={invite.email} readOnly disabled />
        </div>

        <div className="space-y-2">
          <Label htmlFor="name">Name</Label>
          <Input
            id="name"
            autoComplete="name"
            value={data.name}
            onChange={(e) => setData('name', e.target.value)}
            required
          />
          {errors.name && <p className="text-sm font-medium text-destructive">{errors.name}</p>}
        </div>

        <div className="space-y-2">
          <Label htmlFor="password">Password</Label>
          <Input
            id="password"
            type="password"
            autoComplete="new-password"
            value={data.password}
            onChange={(e) => setData('password', e.target.value)}
            required
          />
          {errors.password && <p className="text-sm font-medium text-destructive">{errors.password}</p>}
        </div>

        <div className="space-y-2">
          <Label htmlFor="password_confirmation">Confirm password</Label>
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
          {processing ? 'Saving…' : 'Set password and sign in'}
        </Button>
      </form>
    </AuthLayout>
  )
}
