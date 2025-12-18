import { Link, useForm, usePage } from '@inertiajs/react'
import SettingsLayout from '@/components/layouts/SettingsLayout'
import { Card, CardHeader, CardTitle, CardContent, CardFooter } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { SharedProps } from '@/types'
import { FormEventHandler } from 'react'

interface IntegrationsNewProps {
  providers: { [key: string]: string }
}

export default function IntegrationsNew({ providers }: IntegrationsNewProps) {
  const { current_org } = usePage<SharedProps>().props

  const { data, setData, post, processing, errors } = useForm({
    provider: '',
    name: '',
    settings: {
      webhook_url: '',
      channel: '',
      routing_key: '',
      notify_on_new_issue: true
    }
  })

  const submit: FormEventHandler = (e) => {
    e.preventDefault()
    post(`/${current_org?.slug}/settings/integrations`)
  }

  if (!current_org) return null

  return (
    <SettingsLayout>
      <div className="space-y-8">
        <div>
          <h1 className="text-3xl font-bold tracking-tight mb-1">Add Integration</h1>
          <p className="text-muted-foreground">
            Configure a new integration service for your organization.
          </p>
        </div>

        <Card>
          <form onSubmit={submit}>
            <CardHeader>
              <CardTitle>Integration Details</CardTitle>
            </CardHeader>
            <CardContent className="space-y-6">

              <div className="space-y-2">
                <Label htmlFor="provider">Provider</Label>
                <Select
                  value={data.provider}
                  onValueChange={(val) => setData('provider', val)}
                >
                  <SelectTrigger>
                    <SelectValue placeholder="Select a provider" />
                  </SelectTrigger>
                  <SelectContent>
                    {Object.entries(providers).map(([key, label]) => (
                      <SelectItem key={key} value={key}>{label}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                {errors.provider && (
                  <p className="text-sm font-medium text-destructive">{errors.provider}</p>
                )}
              </div>

              <div className="space-y-2">
                <Label htmlFor="name">Name (Optional)</Label>
                <Input
                  id="name"
                  placeholder="e.g. Engineering Slack"
                  value={data.name}
                  onChange={(e) => setData('name', e.target.value)}
                />
              </div>

              {/* Dynamic fields based on provider */}
              {data.provider === 'slack' && (
                <div className="space-y-4 pt-6 border-t">
                  <h3 className="font-medium text-sm">Slack Configuration</h3>
                  <div className="space-y-2">
                    <Label htmlFor="webhook_url">Webhook URL</Label>
                    <Input
                      id="webhook_url"
                      placeholder="https://hooks.slack.com/services/..."
                      value={data.settings.webhook_url}
                      onChange={(e) => setData('settings', { ...data.settings, webhook_url: e.target.value })}
                      required
                    />
                  </div>
                  <div className="space-y-2">
                    <Label htmlFor="channel">Channel (Optional)</Label>
                    <Input
                      id="channel"
                      placeholder="#alerts"
                      value={data.settings.channel}
                      onChange={(e) => setData('settings', { ...data.settings, channel: e.target.value })}
                    />
                  </div>
                </div>
              )}

              {data.provider === 'pagerduty' && (
                <div className="space-y-4 pt-6 border-t">
                  <h3 className="font-medium text-sm">PagerDuty Configuration</h3>
                  <div className="space-y-2">
                    <Label htmlFor="routing_key">Routing Key / Integration Key</Label>
                    <Input
                      id="routing_key"
                      value={(data.settings as any).routing_key || ''}
                      onChange={(e) => setData('settings', { ...data.settings, routing_key: e.target.value })}
                      required
                    />
                  </div>
                </div>
              )}

            </CardContent>
            <CardFooter className="flex justify-end gap-2 border-t bg-muted/20 px-6 py-4">
              <Link href={`/${current_org.slug}/settings/integrations`}>
                <Button variant="outline" type="button">Cancel</Button>
              </Link>
              <Button type="submit" disabled={processing}>
                {processing ? 'Adding...' : 'Add Integration'}
              </Button>
            </CardFooter>
          </form>
        </Card>
      </div>
    </SettingsLayout>
  )
}
