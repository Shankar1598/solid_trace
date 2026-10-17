import { Link, useForm, usePage } from '@inertiajs/react'
import SettingsLayout from '@/components/layouts/SettingsLayout'
import { PageHeader } from '@/components/PageHeader'
import { Card, CardContent, CardFooter } from '@/components/ui/card'
import { Button, buttonVariants } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Checkbox } from '@/components/ui/checkbox'
import { providerName } from '@/lib/integrations'
import { SharedProps, Integration } from '@/types'
import { FormEventHandler } from 'react'
import { toast } from 'sonner'

interface IntegrationsEditProps {
  integration: Integration
  providers: { [key: string]: string }
}

export default function IntegrationsEdit({ integration }: IntegrationsEditProps) {
  const { current_org } = usePage<SharedProps>().props

  const { data, setData, put, processing, errors } = useForm({
    provider: integration.provider,
    name: integration.name || '',
    active: integration.enabled, // Mapping from serializer's 'enabled' key
    settings: {
      webhook_url: (integration.settings as any)?.webhook_url || '',
      routing_key: (integration.settings as any)?.routing_key || '',
      severity: (integration.settings as any)?.severity || 'error',
      recipients: (integration.settings as any)?.recipients || '',
      notify_on_new_issue: (integration.settings as any)?.notify_on_new_issue ?? true,
      notify_on_assignment: (integration.settings as any)?.notify_on_assignment ?? false,
      notify_on_event_threshold: (integration.settings as any)?.notify_on_event_threshold ?? true,
      event_threshold: (integration.settings as any)?.event_threshold ?? 10,
      time_window_minutes: (integration.settings as any)?.time_window_minutes ?? 5
    }
  })

  const submit: FormEventHandler = (e) => {
    e.preventDefault()
    put(`/${current_org?.slug}/settings/integrations/${integration.id}`, {
      onError: () => toast.error('Failed to update integration')
    })
  }

  if (!current_org) return null

  return (
    <SettingsLayout>
      <PageHeader
        breadcrumbs={[{ label: 'Integrations', href: `/${current_org.slug}/settings/integrations` }, { label: integration.name || providerName(integration.provider) }]}
        title={integration.name || providerName(integration.provider)}
        description={`A ${providerName(integration.provider)} integration.`}
      />
        <Card>
          <form onSubmit={submit}>
            <CardContent className="space-y-6 pb-2">
              <div className="space-y-2">
                <Label htmlFor="name">Name (optional)</Label>
                <Input
                  id="name"
                  placeholder="e.g. Engineering Slack"
                  value={data.name}
                  onChange={(e) => setData('name', e.target.value)}
                />
                {errors.name && (
                  <p className="text-sm font-medium text-destructive">{errors.name}</p>
                )}
              </div>

              {/* Dynamic fields based on provider */}
              {integration.provider === 'slack' && (
                <div className="space-y-6 pt-6 border-t">
                  <h3 className="text-sm font-semibold">Connection</h3>
                  <div className="space-y-2">
                    <Label htmlFor="webhook_url">Webhook URL</Label>
                    <Input
                      id="webhook_url"
                      className="font-mono"
                      placeholder="https://hooks.slack.com/services/..."
                      value={data.settings.webhook_url}
                      onChange={(e) => setData('settings', { ...data.settings, webhook_url: e.target.value })}
                      required
                    />
                    <p className="text-xs text-muted-foreground">
                      The Incoming Webhook URL from your Slack App configuration.
                    </p>
                    {errors['settings.webhook_url'] && (
                      <p className="text-sm font-medium text-destructive">{errors['settings.webhook_url'] as string}</p>
                    )}
                  </div>
                </div>
              )}

              {integration.provider === 'pagerduty' && (
                <div className="space-y-6 pt-6 border-t">
                  <h3 className="text-sm font-semibold">Connection</h3>
                  <div className="space-y-2">
                    <Label htmlFor="routing_key">Routing key</Label>
                    <Input
                      id="routing_key"
                      className="font-mono"
                      placeholder="Your PagerDuty integration key"
                      value={data.settings.routing_key}
                      onChange={(e) => setData('settings', { ...data.settings, routing_key: e.target.value })}
                      required
                    />
                    {errors['settings.routing_key'] && (
                      <p className="text-sm font-medium text-destructive">{errors['settings.routing_key'] as string}</p>
                    )}
                  </div>
                  <div className="space-y-2">
                    <Label htmlFor="severity">Incident severity</Label>
                    <Select
                      value={data.settings.severity}
                      onValueChange={(val) => setData('settings', { ...data.settings, severity: val })}
                    >
                      <SelectTrigger>
                        <SelectValue placeholder="Select severity" />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="info">Info</SelectItem>
                        <SelectItem value="warning">Warning</SelectItem>
                        <SelectItem value="error">Error</SelectItem>
                        <SelectItem value="critical">Critical</SelectItem>
                      </SelectContent>
                    </Select>
                  </div>
                </div>
              )}

              {integration.provider === 'email' && (
                <div className="space-y-6 pt-6 border-t">
                  <h3 className="text-sm font-semibold">Connection</h3>
                  <div className="space-y-2">
                    <Label htmlFor="recipients">Recipients</Label>
                    <Input
                      id="recipients"
                      placeholder="devs@company.com, ops@company.com"
                      value={data.settings.recipients}
                      onChange={(e) => setData('settings', { ...data.settings, recipients: e.target.value })}
                      required
                    />
                    <p className="text-xs text-muted-foreground">
                      Separate multiple email addresses with commas.
                    </p>
                    {errors['settings.recipients'] && (
                      <p className="text-sm font-medium text-destructive">{errors['settings.recipients'] as string}</p>
                    )}
                  </div>
                </div>
              )}

              <div className="space-y-6 pt-6 border-t">
                <h3 className="text-sm font-semibold">When to notify</h3>
                <div className="flex items-center space-x-2">
                  <Checkbox
                    id="notify_on_new_issue"
                    checked={data.settings.notify_on_new_issue}
                    onCheckedChange={(val) => setData('settings', { ...data.settings, notify_on_new_issue: !!val })}
                  />
                  <Label htmlFor="notify_on_new_issue" className="text-sm font-normal">
                    Notify when a new Issue is created
                  </Label>
                </div>

                <div className="flex items-center space-x-2">
                  <Checkbox
                    id="notify_on_assignment"
                    checked={data.settings.notify_on_assignment}
                    onCheckedChange={(val) => setData('settings', { ...data.settings, notify_on_assignment: !!val })}
                  />
                  <Label htmlFor="notify_on_assignment" className="text-sm font-normal">
                    Notify when an Issue is assigned
                  </Label>
                </div>

                <div className="space-y-4">
                  <div className="flex items-center space-x-2">
                    <Checkbox
                      id="notify_on_event_threshold"
                      checked={data.settings.notify_on_event_threshold}
                      onCheckedChange={(val) => setData('settings', { ...data.settings, notify_on_event_threshold: !!val })}
                    />
                    <Label htmlFor="notify_on_event_threshold" className="text-sm font-normal">
                      Notify when the Event count passes the threshold
                    </Label>
                  </div>

                  {data.settings.notify_on_event_threshold && (
                    <div className="grid grid-cols-2 gap-4 pl-6">
                      <div className="space-y-2">
                        <Label htmlFor="event_threshold">Event threshold</Label>
                        <Input
                          id="event_threshold"
                          type="number"
                          value={data.settings.event_threshold}
                          onChange={(e) => setData('settings', { ...data.settings, event_threshold: parseInt(e.target.value) || 0 })}
                        />
                      </div>
                      <div className="space-y-2">
                        <Label htmlFor="time_window_minutes">Time window (minutes)</Label>
                        <Input
                          id="time_window_minutes"
                          type="number"
                          value={data.settings.time_window_minutes}
                          onChange={(e) => setData('settings', { ...data.settings, time_window_minutes: parseInt(e.target.value) || 0 })}
                        />
                      </div>
                    </div>
                  )}
                </div>
              </div>

              <div className="flex items-center space-x-2 pt-6 border-t">
                <Checkbox
                  id="active"
                  checked={data.active}
                  onCheckedChange={(val) => setData('active', !!val)}
                />
                <Label htmlFor="active" className="text-sm font-normal">
                  On: send alerts through this integration
                </Label>
              </div>
            </CardContent>
            <CardFooter className="justify-end gap-2">
              <Link href={`/${current_org.slug}/settings/integrations`} className={buttonVariants({ variant: 'outline' })}>
                Cancel
              </Link>
              <Button type="submit" disabled={processing}>
                {processing ? 'Saving…' : 'Save changes'}
              </Button>
            </CardFooter>
          </form>
        </Card>
    </SettingsLayout>
  )
}
