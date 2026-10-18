import { Link, usePage } from '@inertiajs/react'
import SettingsLayout from '@/components/layouts/SettingsLayout'
import { PageHeader } from '@/components/PageHeader'
import { Card, CardContent } from '@/components/ui/card'
import { Button, buttonVariants } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { providerName } from '@/lib/integrations'
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
      <PageHeader
        title="Integrations"
        description="Where SolidTrace sends alerts: Slack channels, PagerDuty services and email."
        actions={
          <Link href={`/${current_org.slug}/settings/integrations/new`} className={buttonVariants()}>
            <Plus className="h-4 w-4" />
            Add integration
          </Link>
        }
      />

      {integrations.length === 0 ? (
        <Card className="border-dashed">
          <CardContent className="flex flex-col items-center justify-center py-12 text-center">
            <Settings className="mb-4 h-12 w-12 text-muted-foreground opacity-50" />
            <h3 className="text-lg font-medium">No integrations yet</h3>
            <p className="text-sm text-muted-foreground mb-6 max-w-sm">
              Integrations allow you to receive alerts and sync data with external services like Slack or PagerDuty.
            </p>
            <Link href={`/${current_org.slug}/settings/integrations/new`} className={buttonVariants({ variant: 'outline' })}>
              Create your first integration
            </Link>
          </CardContent>
        </Card>
      ) : (
        <div className="border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Provider</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Created</TableHead>
                <TableHead><span className="sr-only">Actions</span></TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {integrations.map((integration) => (
                <TableRow key={integration.id}>
                  <TableCell>
                    <Link href={`/${current_org.slug}/settings/integrations/${integration.id}/edit`} className="font-medium hover:underline">
                      {integration.name || providerName(integration.provider)}
                    </Link>
                  </TableCell>
                  <TableCell>{providerName(integration.provider)}</TableCell>
                  <TableCell>
                    {integration.enabled ? <Badge variant="ok" dot>On</Badge> : <Badge dot>Off</Badge>}
                  </TableCell>
                  <TableCell className="text-muted-foreground">{formatDistanceToNow(new Date(integration.created_at))} ago</TableCell>
                  <TableCell className="text-right">
                    <div className="flex items-center justify-end gap-1">
                      <Link
                        href={`/${current_org.slug}/settings/integrations/${integration.id}/edit`}
                        className={buttonVariants({ variant: 'outline' })}
                      >
                        <Settings className="h-4 w-4" />
                        Configure
                      </Link>
                      <Button
                        variant="ghost"
                        size="icon"
                        aria-label={`Delete ${integration.name || providerName(integration.provider)}`}
                        onClick={() => handleDelete(integration.id)}
                        className="text-muted-foreground hover:text-destructive"
                      >
                        <Trash2 className="h-4 w-4" />
                      </Button>
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </SettingsLayout>
  )
}
