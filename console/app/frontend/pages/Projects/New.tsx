import { Link, useForm, usePage } from '@inertiajs/react'
import DashboardLayout from '@/components/layouts/DashboardLayout'
import { Card, CardHeader, CardTitle, CardContent, CardDescription, CardFooter } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { SharedProps } from '@/types'
import { FormEventHandler } from 'react'

export default function ProjectsNew() {
  const { current_org } = usePage<SharedProps>().props

  const { data, setData, post, processing, errors } = useForm({
    name: '',
    platform: 'javascript'
  })

  // Common platforms list
  const platforms = [
    { value: 'javascript', label: 'JavaScript (Browser)' },
    { value: 'node', label: 'Node.js' },
    { value: 'python', label: 'Python' },
    { value: 'ruby', label: 'Ruby / Rails' },
    { value: 'go', label: 'Go' },
    { value: 'java', label: 'Java' },
    { value: 'other', label: 'Other' },
  ]

  const submit: FormEventHandler = (e) => {
    e.preventDefault()
    post(`/${current_org?.slug}/projects`)
  }

  if (!current_org) return null

  return (
    <DashboardLayout>
      <div className="max-w-2xl mx-auto space-y-6">
        <div className="space-y-0.5">
          <h1 className="text-2xl font-bold tracking-tight">Create New Project</h1>
          <p className="text-muted-foreground">
            Create a new project to start tracking errors for your application.
          </p>
        </div>

        <Card>
          <form onSubmit={submit}>
            <CardHeader>
              <CardTitle>Project Details</CardTitle>
              <CardDescription>
                Provide a name and select the platform for your project.
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="space-y-2">
                <Label htmlFor="name">Project Name</Label>
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
                <Label htmlFor="platform">Platform</Label>
                <Select
                  value={data.platform}
                  onValueChange={(val) => setData('platform', val ?? 'ruby')}
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
            <CardFooter className="flex justify-end gap-2">
              <Link href={`/${current_org.slug}/projects`}>
                <Button variant="outline" type="button">Cancel</Button>
              </Link>
              <Button type="submit" disabled={processing}>
                {processing ? 'Creating...' : 'Create Project'}
              </Button>
            </CardFooter>
          </form>
        </Card>
      </div>
    </DashboardLayout>
  )
}
