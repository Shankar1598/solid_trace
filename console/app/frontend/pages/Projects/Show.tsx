import { usePage } from '@inertiajs/react'
import DashboardLayout from '@/components/layouts/DashboardLayout'
import Breadcrumbs from '@/components/Breadcrumbs'
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Project, SharedProps } from '@/types'
import { Copy, RefreshCw, AlertTriangle } from 'lucide-react'
import { useState } from 'react'

interface ProjectsShowProps {
  project: Project
}

export default function ProjectsShow({ project }: ProjectsShowProps) {
  const { current_org } = usePage<SharedProps>().props
  const [copiedKey, setCopiedKey] = useState<string | null>(null)

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
                    <Button variant="outline" size="sm">
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
