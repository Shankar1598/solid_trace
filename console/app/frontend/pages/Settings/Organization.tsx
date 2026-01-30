import { useForm } from '@inertiajs/react'
import SettingsLayout from '@/components/layouts/SettingsLayout'
import { Card, CardHeader, CardTitle, CardContent, CardDescription, CardFooter } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Organization } from '@/types'

interface SettingsOrganizationProps {
  organization: Organization
}

export default function SettingsOrganization({ organization }: SettingsOrganizationProps) {
  const { data, setData, put, processing, errors } = useForm({
    name: organization.name
  })

  const submitOrg: React.FormEventHandler = (e) => {
    e.preventDefault()
    put(`/${organization.slug}/settings/organization`, {
      preserveScroll: true
    })
  }

  return (
    <SettingsLayout>
      <div className="space-y-8">
        <div>
          <h1 className="text-3xl font-bold tracking-tight mb-1">Organization Settings</h1>
          <p className="text-muted-foreground">
            Manage your organization profile and public identifiers.
          </p>
        </div>

        <div className="space-y-6">
          <Card>
            <form onSubmit={submitOrg}>
              <CardHeader>
                <CardTitle>General Settings</CardTitle>
                <CardDescription>
                  Manage your organization profile.
                </CardDescription>
              </CardHeader>
              <CardContent className="space-y-4">
                <div className="space-y-2">
                  <Label htmlFor="orgName">Organization Name</Label>
                  <Input
                    id="orgName"
                    value={data.name}
                    onChange={(e) => setData('name', e.target.value)}
                  />
                  {errors.name && (
                    <p className="text-sm font-medium text-destructive">{errors.name}</p>
                  )}
                </div>
                <div className="space-y-2">
                  <Label>Slug</Label>
                  <Input value={organization.slug} disabled className="font-mono bg-muted" />
                  <p className="text-xs text-muted-foreground">
                    The slug is used in URLs and cannot be changed.
                  </p>
                </div>
              </CardContent>
              <CardFooter className="flex justify-end border-t bg-muted/20 px-6 py-4">
                <Button type="submit" disabled={processing}>
                  {processing ? 'Saving...' : 'Save Changes'}
                </Button>
              </CardFooter>
            </form>
          </Card>
        </div>
      </div>
    </SettingsLayout>
  )
}
