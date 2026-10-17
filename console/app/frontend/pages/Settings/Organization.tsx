import { useForm } from '@inertiajs/react'
import SettingsLayout from '@/components/layouts/SettingsLayout'
import { PageHeader } from '@/components/PageHeader'
import { Card, CardHeader, CardTitle, CardContent, CardFooter } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Organization } from '@/types'
import { toast } from 'sonner'

interface SettingsOrganizationProps {
  organization: Organization
}

export default function SettingsOrganization({ organization }: SettingsOrganizationProps) {
  const { data, setData, put, processing, errors, transform } = useForm({
    name: organization.name,
    slug: organization.slug
  })

  transform((data) => ({
    organization: data
  }))

  const submitOrg: React.FormEventHandler = (e) => {
    e.preventDefault()
    put(`/${organization.slug}/settings/organization`, {
      preserveScroll: true,
      onError: () => toast.error('Failed to update organization settings')
    })
  }

  return (
    <SettingsLayout>
      <PageHeader title="Organization" description="Its name, and the slug in every link to it." />
      <Card>
        <form onSubmit={submitOrg}>
          <CardHeader>
            <CardTitle>General</CardTitle>
          </CardHeader>
          <CardContent className="space-y-6 pb-2">
            <div className="space-y-2">
              <Label htmlFor="orgName">Name</Label>
              <Input
                id="orgName"
                value={data.name}
                onChange={(e) => setData('name', e.target.value)}
                placeholder="Enter organization name"
              />
              {errors.name && (
                <p className="text-sm font-medium text-destructive">{errors.name}</p>
              )}
            </div>

            <div className="space-y-2">
              <Label htmlFor="slug">Slug</Label>
              <Input
                id="slug"
                value={data.slug}
                onChange={(e) => setData('slug', e.target.value)}
                className="font-mono"
              />
              {errors.slug && (
                <p className="text-sm font-medium text-destructive">{errors.slug}</p>
              )}
              <p className="text-xs text-muted-foreground">
                The slug is used in URLs. Changing it will break existing links.
              </p>
            </div>
          </CardContent>
          <CardFooter className="justify-end">
            <Button type="submit" disabled={processing}>
              {processing ? 'Saving…' : 'Save changes'}
            </Button>
          </CardFooter>
        </form>
      </Card>
    </SettingsLayout>
  )
}
