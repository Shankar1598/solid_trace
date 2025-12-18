import { Link, useForm, usePage } from '@inertiajs/react'
import SettingsLayout from '@/components/layouts/SettingsLayout'
import { Card, CardHeader, CardTitle, CardContent, CardFooter } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { SharedProps, Integration } from '@/types'
import { FormEventHandler } from 'react'

interface IntegrationsEditProps {
  integration: Integration
  providers: { [key: string]: string }
}

export default function IntegrationsEdit({ integration, providers }: IntegrationsEditProps) {
  const { current_org } = usePage<SharedProps>().props

  const { data, setData, put, processing } = useForm({
    provider: integration.provider,
    name: integration.name || '',
    active: integration.enabled,
    settings: {
      webhook_url: (integration.settings as any)?.webhook_url || '',
      channel: (integration.settings as any)?.channel || '',
      routing_key: (integration.settings as any)?.routing_key || '',
      notify_on_new_issue: (integration.settings as any)?.notify_on_new_issue ?? true
    }
  })

  const submit: FormEventHandler = (e) => {
    e.preventDefault()
    put(`/${current_org?.slug}/settings/integrations/${integration.id}`)
  }

  if (!current_org) return null

  return (
    <SettingsLayout>
      <div className="space-y-8">
        <div>
          <h1 className="text-3xl font-bold tracking-tight mb-1">Edit Integration</h1>
          <p className="text-muted-foreground">
            Update settings for your {providers[integration.provider] || integration.provider} integration.
          </p>
        </div>

        <Card>
          <form onSubmit={submit}>
            <CardHeader>
              <CardTitle>{providers[integration.provider] || integration.provider} Configuration</CardTitle>
            </CardHeader>
            <CardContent className="space-y-6">
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
              {integration.provider === 'slack' && (
                <div className="space-y-4 pt-6 border-t">
                  <h3 className="font-medium text-sm">Slack Settings</h3>
                  <div className="space-y-2">
                    <Label htmlFor="webhook_url">Webhook URL</Label>
                    <Input
                      id="webhook_url"
                      value={data.settings.webhook_url}
                      onChange={(e) => setData('settings', { ...data.settings, webhook_url: e.target.value })}
                      required
                    />
                  </div>
                  <div className="space-y-2">
                    <Label htmlFor="channel">Channel (Optional)</Label>
                    <Input
                      id="channel"
                      value={data.settings.channel}
                      onChange={(e) => setData('settings', { ...data.settings, channel: e.target.value })}
                    />
                  </div>
                </div>
              )}

              {integration.provider === 'pagerduty' && (
                <div className="space-y-4 pt-6 border-t">
                  <h3 className="font-medium text-sm">PagerDuty Settings</h3>
                  <div className="space-y-2">
                    <Label htmlFor="routing_key">Routing Key / Integration Key</Label>
                    <Input
                      id="routing_key"
                      value={data.settings.routing_key}
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
                {processing ? 'Saving...' : 'Save Changes'}
              </Button>
            </CardFooter>
          </form>
        </Card>
      </div>
    </SettingsLayout>
  )
}
