import { Link, useForm, usePage } from '@inertiajs/react'
import SettingsLayout from '@/components/layouts/SettingsLayout'
import { Card, CardHeader, CardTitle, CardContent, CardFooter } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Checkbox } from '@/components/ui/checkbox'
import { SharedProps, Integration } from '@/types'
import { FormEventHandler } from 'react'
import { toast } from 'sonner'

interface IntegrationsEditProps {
  integration: Integration
  providers: { [key: string]: string }
}

export default function IntegrationsEdit({ integration, providers }: IntegrationsEditProps) {
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
      notify_on_event_threshold: (integration.settings as any)?.notify_on_event_threshold ?? false,
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
            <CardContent className="space-y-6 pb-6">
              <div className="space-y-2">
                <Label htmlFor="name">Integration Name (Optional)</Label>
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
                  <h3 className="font-medium text-sm">PagerDuty Configuration</h3>
                  <div className="space-y-2">
                    <Label htmlFor="routing_key">Routing Key</Label>
                    <Input
                      id="routing_key"
                      placeholder="Enter your PagerDuty Integration Key"
                      value={data.settings.routing_key}
                      onChange={(e) => setData('settings', { ...data.settings, routing_key: e.target.value })}
                      required
                    />
                    {errors['settings.routing_key'] && (
                      <p className="text-sm font-medium text-destructive">{errors['settings.routing_key'] as string}</p>
                    )}
                  </div>
                  <div className="space-y-2">
                    <Label htmlFor="severity">Incident Severity</Label>
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
                  <h3 className="font-medium text-sm">Email Configuration</h3>
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
                <h3 className="font-medium text-sm">Common Notification Rules</h3>
                <div className="flex items-center space-x-2">
                  <Checkbox
                    id="notify_on_new_issue"
                    checked={data.settings.notify_on_new_issue}
                    onCheckedChange={(val) => setData('settings', { ...data.settings, notify_on_new_issue: !!val })}
                  />
                  <Label htmlFor="notify_on_new_issue" className="text-sm font-normal">
                    Notify when a new issue is created
                  </Label>
                </div>

                <div className="flex items-center space-x-2">
                  <Checkbox
                    id="notify_on_assignment"
                    checked={data.settings.notify_on_assignment}
                    onCheckedChange={(val) => setData('settings', { ...data.settings, notify_on_assignment: !!val })}
                  />
                  <Label htmlFor="notify_on_assignment" className="text-sm font-normal">
                    Notify when an issue is assigned
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
                      Notify when event count exceeds threshold
                    </Label>
                  </div>

                  {data.settings.notify_on_event_threshold && (
                    <div className="grid grid-cols-2 gap-4 pl-6">
                      <div className="space-y-2">
                        <Label htmlFor="event_threshold">Event Threshold</Label>
                        <Input
                          id="event_threshold"
                          type="number"
                          value={data.settings.event_threshold}
                          onChange={(e) => setData('settings', { ...data.settings, event_threshold: parseInt(e.target.value) || 0 })}
                        />
                      </div>
                      <div className="space-y-2">
                        <Label htmlFor="time_window_minutes">Time Window (Minutes)</Label>
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
                  Integration is active and enabled
                </Label>
              </div>
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
