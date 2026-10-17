import { router, useForm, usePage } from '@inertiajs/react'
import DashboardLayout from '@/components/layouts/DashboardLayout'
import Breadcrumbs from '@/components/Breadcrumbs'
import { Card, CardContent, CardHeader, CardTitle, CardDescription, CardFooter } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Project, SharedProps } from '@/types'
import { Copy, RefreshCw, AlertTriangle } from 'lucide-react'
import { useState } from 'react'
import { toast } from 'sonner'

interface ProjectsShowProps {
  project: Project
  default_log_retention_days: number
  can_manage_project: boolean
}

export default function ProjectsShow({ project, default_log_retention_days, can_manage_project }: ProjectsShowProps) {
  const { current_org } = usePage<SharedProps>().props
  const [copiedKey, setCopiedKey] = useState<string | null>(null)
  const { data, setData, patch, processing, errors, transform } = useForm({
    log_retention_days: project.log_retention_days?.toString() ?? ''
  })

  transform((data) => ({
    project: data
  }))

  if (!current_org) return null

  const breadcrumbItems = [
    { label: 'Projects', href: `/${current_org.slug}/projects` },
    { label: project.name }
  ]

  const copyToClipboard = (text: string, label: string) => {
    navigator.clipboard.writeText(text)
    setCopiedKey(label)
    setTimeout(() => setCopiedKey(null), 2000)
  }

  const submitRetention: React.FormEventHandler = (e) => {
    e.preventDefault()
    patch(`/${current_org.slug}/projects/${project.slug}`, {
      preserveScroll: true,
      onError: () => toast.error('Failed to update Log Retention')
    })
  }

  // Find the active DSN (usually the first one created usually, or we'd have a flag)
  // For now just taking the first one
  const activeKey = project.keys && project.keys.length > 0 ? project.keys[0] : null

  return (
    <DashboardLayout breadcrumbs={<Breadcrumbs items={breadcrumbItems} />}>
      <div className="space-y-6">
        <div className="flex items-center justify-between">
          <h1 className="text-3xl font-bold tracking-tight">{project.name}</h1>
        </div>

        <div className="grid gap-6 md:grid-cols-2">
          <div className="space-y-6">
            <Card>
              <CardHeader>
                <CardTitle>Project Configuration</CardTitle>
                <CardDescription>
                  Configure your project settings and integration.
                </CardDescription>
              </CardHeader>
              <CardContent className="space-y-4">
                <div className="space-y-2">
                  <Label>Platform</Label>
                  <Input value={project.platform || 'Not set'} disabled />
                </div>
                <div className="space-y-2">
                  <Label>Project ID</Label>
                  <Input value={project.id} disabled className="font-mono" />
                </div>
              </CardContent>
            </Card>

            <Card>
              <form onSubmit={submitRetention}>
                <CardHeader>
                  <CardTitle>Log Retention</CardTitle>
                  <CardDescription>
                    How many whole days this project's logs are kept before they are deleted.
                  </CardDescription>
                </CardHeader>
                <CardContent className="space-y-2 pb-6">
                  <Label htmlFor="logRetentionDays">Days</Label>
                  <Input
                    id="logRetentionDays"
                    type="number"
                    min={1}
                    step={1}
                    inputMode="numeric"
                    value={data.log_retention_days}
                    onChange={(e) => setData('log_retention_days', e.target.value)}
                    placeholder={`Instance default (${default_log_retention_days})`}
                    disabled={!can_manage_project}
                  />
                  {errors.log_retention_days && (
                    <p className="text-sm font-medium text-destructive">{errors.log_retention_days}</p>
                  )}
                  <p className="text-xs text-muted-foreground">
                    Leave empty to use the instance default of {default_log_retention_days} days.
                    {!can_manage_project && ' Only organization admins can change this.'}
                  </p>
                </CardContent>
                {can_manage_project && (
                  <CardFooter className="flex justify-end border-t bg-muted/20 px-6 py-4">
                    <Button type="submit" disabled={processing}>
                      {processing ? 'Saving...' : 'Save'}
                    </Button>
                  </CardFooter>
                )}
              </form>
            </Card>

            <Card className="border-destructive/50">
              <CardHeader>
                <CardTitle className="text-destructive">Danger Zone</CardTitle>
              </CardHeader>
              <CardContent>
                <p className="text-sm text-muted-foreground mb-4">
                  Deleting this project will permanently remove all issues and events associated with it.
                </p>
                <Button variant="destructive">Delete Project</Button>
              </CardContent>
            </Card>
          </div>

          <div className="space-y-6">
            <Card>
              <CardHeader>
                <CardTitle>Client Keys (DSN)</CardTitle>
                <CardDescription>
                  Use this DSN to configure your SDK.
                </CardDescription>
              </CardHeader>
              <CardContent className="space-y-6">
                {activeKey ? (
                  <div className="space-y-4">
                    <div className="space-y-2">
                      <Label>Public DSN</Label>
                      <div className="flex gap-2">
                        <Input value={activeKey.dsn} readOnly className="font-mono text-xs" />
                        <Button
                          variant="outline"
                          size="icon"
                          onClick={() => copyToClipboard(activeKey.dsn, 'dsn')}
                        >
                          {copiedKey === 'dsn' ? <span className="text-xs">Copied</span> : <Copy className="h-4 w-4" />}
                        </Button>
                      </div>
                    </div>
                    {/* Could show Public Key separately if needed, but DSN usually contains it */}
                  </div>
                ) : (
                  <div className="flex flex-col items-center justify-center p-6 text-center border-2 border-dashed rounded-lg">
                    <AlertTriangle className="h-8 w-8 text-yellow-500 mb-2" />
                    <h3 className="font-medium">No Client Keys Found</h3>
                    <p className="text-sm text-muted-foreground mb-4">
                      This project doesn't have any client keys generated yet.
                    </p>
                    <Button variant="outline" size="sm" onClick={() => router.post(`/${current_org.slug}/projects/${project.slug}/keys`)}>
                      <RefreshCw className="mr-2 h-4 w-4" />
                      Generate New Key
                    </Button>
                  </div>
                )}
              </CardContent>
            </Card>
          </div>
        </div>
      </div>
    </DashboardLayout>
  )
}
