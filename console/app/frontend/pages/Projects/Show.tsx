import { router, useForm, usePage } from '@inertiajs/react'
import DashboardLayout from '@/components/layouts/DashboardLayout'
import { PageHeader } from '@/components/PageHeader'
import { Card, CardContent, CardHeader, CardTitle, CardDescription, CardFooter } from '@/components/ui/card'
import {
  AlertDialog, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Project, SharedProps } from '@/types'
import { AlertTriangle, Check, Copy, RefreshCw } from 'lucide-react'
import { useState } from 'react'
import { toast } from 'sonner'

interface ProjectsShowProps {
  project: Project
  default_log_retention_days: number
  can_manage_project: boolean
  can_manage_keys: boolean
  can_delete_project: boolean
}

export default function ProjectsShow({ project, default_log_retention_days, can_manage_project, can_manage_keys, can_delete_project }: ProjectsShowProps) {
  const { current_org } = usePage<SharedProps>().props
  const [copiedKey, setCopiedKey] = useState<string | null>(null)
  const [deleting, setDeleting] = useState(false)
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
    <DashboardLayout>
      <PageHeader breadcrumbs={breadcrumbItems} title={project.name} description={<span className="font-mono text-xs">{project.slug}</span>} />
      {/* One column, the DSN first: it's what a new Project is opened for. */}
      <div className="max-w-4xl space-y-6">
        <Card>
          <CardHeader>
            <CardTitle>Client keys</CardTitle>
            <CardDescription>
              Use this DSN to configure your SDK.
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-6">
            {activeKey ? (
              <div className="space-y-4">
                <div className="space-y-2">
                  <Label>DSN</Label>
                  <div className="flex gap-2">
                    <Input value={activeKey.dsn} readOnly className="font-mono text-xs" />
                    <Button
                      variant="outline"
                      size="icon"
                      aria-label={copiedKey === 'dsn' ? 'Copied the DSN' : 'Copy the DSN'}
                      title={copiedKey === 'dsn' ? 'Copied' : 'Copy'}
                      onClick={() => copyToClipboard(activeKey.dsn, 'dsn')}
                    >
                      {copiedKey === 'dsn' ? <Check className="h-4 w-4" /> : <Copy className="h-4 w-4" />}
                    </Button>
                  </div>
                </div>
                {/* Could show Public Key separately if needed, but DSN usually contains it */}
              </div>
            ) : (
              <div className="flex flex-col items-center justify-center p-6 text-center border border-dashed">
                <AlertTriangle className="h-8 w-8 text-warning mb-2" />
                <h3 className="font-medium">No client keys yet</h3>
                <p className="text-sm text-muted-foreground mb-4">
                  This project doesn't have any client keys generated yet.
                </p>
                {can_manage_keys ? (
                  <Button variant="outline" onClick={() => router.post(`/${current_org.slug}/projects/${project.slug}/keys`)}>
                    <RefreshCw className="h-4 w-4" />
                    Generate a key
                  </Button>
                ) : (
                  <p className="text-xs text-muted-foreground">Only an Organization admin can generate one.</p>
                )}
              </div>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Details</CardTitle>
          </CardHeader>
          <CardContent>
            <dl className="grid grid-cols-[8rem_1fr] gap-y-2 text-sm">
              <dt className="text-muted-foreground">Platform</dt>
              <dd className="font-mono text-xs leading-5">{project.platform || <span className="font-sans text-muted-foreground">Not set</span>}</dd>
              <dt className="text-muted-foreground">Project ID</dt>
              <dd className="font-mono text-xs leading-5">{project.id}</dd>
            </dl>
          </CardContent>
        </Card>

        <Card>
          <form onSubmit={submitRetention}>
            <CardHeader>
              <CardTitle>Log retention</CardTitle>
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
              <CardFooter className="justify-end">
                <Button type="submit" disabled={processing}>
                  {processing ? 'Saving…' : 'Save'}
                </Button>
              </CardFooter>
            )}
          </form>
        </Card>

        {can_delete_project && (
          <Card className="border-destructive/50">
            <CardHeader>
              <CardTitle className="text-destructive">Danger zone</CardTitle>
              <CardDescription>
                Deleting this project permanently removes its issues, keys, monitors and dashboard charts, and its logs within a day.
              </CardDescription>
            </CardHeader>
            <CardContent>
              <Button variant="destructive" onClick={() => setDeleting(true)}>Delete project</Button>
            </CardContent>
          </Card>
        )}
      </div>

      <AlertDialog open={deleting} onOpenChange={setDeleting}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete "{project.name}"?</AlertDialogTitle>
            <AlertDialogDescription>
              Its issues, keys, monitors and dashboard charts are deleted for everyone in the Organization, and SDKs sending with its keys are refused. EventStore deletes its logs within a day.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <Button variant="destructive" onClick={() => router.delete(`/${current_org.slug}/projects/${project.slug}`)}>Delete project</Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </DashboardLayout>
  )
}
