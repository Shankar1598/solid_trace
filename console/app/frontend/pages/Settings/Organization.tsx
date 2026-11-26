import { Link, router, useForm } from '@inertiajs/react'
import SettingsLayout from '@/components/layouts/SettingsLayout'
import { PageHeader } from '@/components/PageHeader'
import { Card, CardHeader, CardTitle, CardContent, CardFooter } from '@/components/ui/card'
import { Button, buttonVariants } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import type { IntegrationOption } from '@/lib/monitors/monitor'
import { monitorStatusItems, monitorStatusParam, NO_MONITOR_STATUS } from '@/lib/monitors/monitorStatus'
import { Organization } from '@/types'
import { toast } from 'sonner'

// Require two-factor (OrganizationSettingsController): whether it's on,
// whether this user may change it (an admin who has Two-factor), and how
// many members it asks to set it up.
interface RequireTwoFactor {
  on: boolean
  can_change: boolean
  // Whether the user viewing the page has Two-factor.
  you_have_two_factor: boolean
  members_without_two_factor: number
}

// The Monitor status Integration (OrganizationSettingsController): the one
// chosen, or null, and those that can be chosen. The Factory provider is not one.
interface MonitorStatusSetting {
  integration_id: number | null
  options: IntegrationOption[]
}

interface SettingsOrganizationProps {
  organization: Organization
  require_two_factor: RequireTwoFactor
  monitor_status: MonitorStatusSetting
}

export default function SettingsOrganization({ organization, require_two_factor, monitor_status }: SettingsOrganizationProps) {
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
      <MonitorStatusCard organization={organization} setting={monitor_status} />
      <RequireTwoFactorCard organization={organization} requirement={require_two_factor} />
    </SettingsLayout>
  )
}

function MonitorStatusCard({ organization, setting }: { organization: Organization; setting: MonitorStatusSetting }) {
  const items = monitorStatusItems(setting.options)
  const value = setting.integration_id === null ? NO_MONITOR_STATUS : String(setting.integration_id)

  const change = (next: string | null) => {
    router.put(`/${organization.slug}/settings/organization`, { organization: { monitor_status_integration_id: monitorStatusParam(next) } }, {
      preserveScroll: true,
      onError: () => toast.error('Failed to change the Monitor status Integration'),
    })
  }

  return (
    <Card className="mt-6" role="region" aria-label="Monitor status">
      <CardHeader>
        <CardTitle>Monitor status</CardTitle>
      </CardHeader>
      <CardContent className="space-y-2 pb-6 text-sm">
        <p>
          The Integration that is told when Monitors do not run for 2 minutes, and when they run again.
          Each Monitor’s own Integrations are not told.
        </p>
        <div className="max-w-sm space-y-2">
          <Label htmlFor="monitorStatusIntegration">Integration</Label>
          <Select value={value} onValueChange={change} items={items}>
            <SelectTrigger id="monitorStatusIntegration" data-monitor-status-integration>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {items.map(item => (
                <SelectItem key={item.value} value={item.value}>{item.label}</SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <p className="text-muted-foreground">
          Slack, PagerDuty and email Integrations only. With None, the Monitors list still shows each Monitor that is not running.
        </p>
      </CardContent>
    </Card>
  )
}

function RequireTwoFactorCard({ organization, requirement }: { organization: Organization; requirement: RequireTwoFactor }) {
  const without = requirement.members_without_two_factor
  const members = without === 1 ? '1 member doesn’t' : `${without} members don’t`

  const change = (on: boolean) => {
    router.put(`/${organization.slug}/settings/organization`, { organization: { require_two_factor: on } }, { preserveScroll: true })
  }

  return (
    <Card className="mt-6" role="region" aria-label="Require two-factor">
      <CardHeader>
        <CardTitle>Require two-factor</CardTitle>
      </CardHeader>
      <CardContent className="space-y-2 pb-2 text-sm">
        <p>
          {requirement.on ? 'On. ' : 'Off. '}
          While it’s on, members without Two-factor are asked to set it up before they can open this Organization,
          and their API tokens are refused on it.
        </p>
        <p className="text-muted-foreground">
          {without === 0
            ? 'Every member has Two-factor.'
            : `${members} have Two-factor yet, and ${requirement.on ? 'will' : 'would'} be asked to set it up.`}
        </p>
        {!requirement.can_change && (
          <p className="text-muted-foreground">
            {requirement.you_have_two_factor
              ? 'Only an admin can change this.'
              : 'Only an admin who has Two-factor can change this. Set yours up first.'}
          </p>
        )}
      </CardContent>
      <CardFooter className="justify-end">
        {requirement.can_change ? (
          requirement.on ? (
            <Button variant="outline" onClick={() => change(false)}>Stop requiring Two-factor</Button>
          ) : (
            <Button onClick={() => change(true)}>Require two-factor</Button>
          )
        ) : (
          !requirement.you_have_two_factor && (
            <Link href={`/authenticator_app/new?org=${organization.slug}`} className={buttonVariants({ variant: 'outline' })}>
              Set up Two-factor
            </Link>
          )
        )}
      </CardFooter>
    </Card>
  )
}
