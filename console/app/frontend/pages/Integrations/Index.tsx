import { Link, usePage } from '@inertiajs/react'
import SettingsLayout from '@/components/layouts/SettingsLayout'
import { Card, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Integration, SharedProps } from '@/types'
import { Plus, Settings, Trash2 } from 'lucide-react'
import { formatDistanceToNow } from 'date-fns'
import { router } from '@inertiajs/react'
import { toast } from 'sonner'

interface IntegrationsIndexProps {
  integrations: Integration[]
}

export default function IntegrationsIndex({ integrations }: IntegrationsIndexProps) {
  const { current_org } = usePage<SharedProps>().props

  if (!current_org) return null

  const handleDelete = (id: number) => {
    if (confirm('Are you sure you want to delete this integration?')) {
      router.delete(`/${current_org.slug}/settings/integrations/${id}`, {
        onError: () => toast.error('Failed to delete integration')
      })
    }
  }

  return (
    <SettingsLayout>
      <div className="space-y-8">
        <div className="flex items-center justify-between">
          <div>
            <h1 className="text-3xl font-bold tracking-tight mb-1">Integrations</h1>
            <p className="text-muted-foreground">
              Connect SolidTrace with external services and alerts.
            </p>
          </div>
          <Link href={`/${current_org.slug}/settings/integrations/new`}>
            <Button>
              <Plus className="mr-2 h-4 w-4" />
              Add Integration
            </Button>
          </Link>
        </div>

        <div className="grid gap-4">
          {integrations.length === 0 ? (
            <Card className="border-dashed bg-muted/20">
              <CardContent className="flex flex-col items-center justify-center py-12 text-center">
                <div className="bg-muted rounded-full p-4 mb-4">
                  <Settings className="h-8 w-8 text-muted-foreground" />
                </div>
                <h3 className="text-lg font-medium">No integrations yet</h3>
                <p className="text-sm text-muted-foreground mb-6 max-w-sm">
                  Integrations allow you to receive alerts and sync data with external services like Slack or PagerDuty.
                </p>
                <Link href={`/${current_org.slug}/settings/integrations/new`}>
                  <Button variant="outline">Create your first integration</Button>
                </Link>
              </CardContent>
            </Card>
          ) : (
            integrations.map((integration) => (
              <Card key={integration.id} className="hover:border-primary/20 transition-colors">
                <CardContent className="p-6 flex items-center justify-between">
                  <div className="flex items-center gap-4">
                    <div className="h-12 w-12 rounded-xl bg-primary/5 flex items-center justify-center font-bold text-primary border border-primary/10">
                      {integration.provider.substring(0, 2).toUpperCase()}
                    </div>
                    <div>
                      <div className="flex items-center gap-2">
                        <h3 className="font-semibold text-lg">{integration.name || integration.provider}</h3>
                        {!integration.enabled && (
                          <Badge variant="secondary" className="text-[10px] h-4 uppercase tracking-wider">Disabled</Badge>
                        )}
                      </div>
                      <p className="text-sm text-muted-foreground capitalize">
                        {integration.provider} Integration
                      </p>
                    </div>
                  </div>

                  <div className="flex items-center gap-3">
                    <span className="text-xs text-muted-foreground mr-2 hidden lg:inline">
                      Created {formatDistanceToNow(new Date(integration.created_at))} ago
                    </span>
                    <Link href={`/${current_org.slug}/settings/integrations/${integration.id}/edit`}>
                      <Button variant="outline" size="sm" className="h-9">
                        <Settings className="h-4 w-4 mr-2" />
                        Configure
                      </Button>
                    </Link>
                    <Button
                      variant="ghost"
                      size="icon"
                      onClick={() => handleDelete(integration.id)}
                      className="h-9 w-9 text-muted-foreground hover:text-destructive hover:bg-destructive/10"
                    >
                      <Trash2 className="h-4 w-4" />
                    </Button>
                  </div>
                </CardContent>
              </Card>
            ))
          )}
        </div>
      </div>
    </SettingsLayout>
  )
}
