import { Link, useForm, usePage } from '@inertiajs/react'
import DashboardLayout from '@/components/layouts/DashboardLayout'
import { PageHeader } from '@/components/PageHeader'
import { Card, CardContent, CardFooter } from '@/components/ui/card'
import { Button, buttonVariants } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { SharedProps } from '@/types'
import { FormEventHandler } from 'react'
import { toast } from 'sonner'

export default function ProjectsNew() {
  const { current_org } = usePage<SharedProps>().props

  const { data, setData, post, processing, errors } = useForm({
    name: '',
    description: '',
    platform: 'javascript'
  })

  // Common platforms list
  const platforms = [
    { value: 'ruby', label: 'Ruby / Rails' },
    { value: 'javascript', label: 'JavaScript (Browser)' },
    { value: 'node', label: 'Node.js' },
    { value: 'go', label: 'Go' },
    { value: 'python', label: 'Python' },
    { value: 'java', label: 'Java' },
    { value: 'other', label: 'Other' },
  ]

  const submit: FormEventHandler = (e) => {
    e.preventDefault()
    post(`/${current_org?.slug}/projects`, {
      onError: () => toast.error('Failed to create project')
    })
  }

  if (!current_org) return null

  return (
    <DashboardLayout>
      <PageHeader
        breadcrumbs={[{ label: 'Projects', href: `/${current_org.slug}/projects` }, { label: 'New project' }]}
        title="New project"
        description="A Project collects one application's errors and Logs."
      />
      <Card className="max-w-4xl">
        <form onSubmit={submit}>
          <CardContent className="space-y-6 pb-2">
            <div className="space-y-2">
              <Label htmlFor="name">Name</Label>
              <Input
                id="name"
                placeholder="e.g. My Awesome App"
                value={data.name}
                onChange={(e) => setData('name', e.target.value)}
                required
              />
              {errors.name && (
                <p className="text-sm font-medium text-destructive">{errors.name}</p>
              )}
            </div>

            <div className="space-y-2">
              <Label htmlFor="description">Description</Label>
              <Textarea
                id="description"
                placeholder="e.g. The checkout service's errors and Logs"
                value={data.description}
                onChange={(e) => setData('description', e.target.value)}
                maxLength={1000}
              />
              {errors.description && (
                <p className="text-sm font-medium text-destructive">{errors.description}</p>
              )}
              <p className="text-xs text-muted-foreground">
                What this project holds, so agents querying over MCP can tell which project to read.
              </p>
            </div>

            <div className="space-y-2">
              <Label htmlFor="platform">Platform</Label>
              <Select
                value={data.platform}
                onValueChange={(val) => setData('platform', val ?? 'ruby')}
                items={platforms}
              >
                <SelectTrigger>
                  <SelectValue placeholder="Select a platform" />
                </SelectTrigger>
                <SelectContent>
                  {platforms.map(p => (
                    <SelectItem key={p.value} value={p.value}>{p.label}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
              {errors.platform && (
                <p className="text-sm font-medium text-destructive">{errors.platform}</p>
              )}
            </div>
          </CardContent>
          <CardFooter className="justify-end gap-2">
            <Link href={`/${current_org.slug}/projects`} className={buttonVariants({ variant: 'outline' })}>
              Cancel
            </Link>
            <Button type="submit" disabled={processing}>
              {processing ? 'Creating…' : 'Create project'}
            </Button>
          </CardFooter>
        </form>
      </Card>
    </DashboardLayout>
  )
}
