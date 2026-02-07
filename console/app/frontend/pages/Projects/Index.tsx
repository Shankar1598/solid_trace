import { Link } from '@inertiajs/react'
import DashboardLayout from '@/components/layouts/DashboardLayout'
import { Card, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Plus, Box, Hash } from 'lucide-react'
import { Project } from '@/types'
import { formatDistanceToNow } from 'date-fns'
import { usePage } from '@inertiajs/react'
import { SharedProps } from '@/types'

interface ProjectsIndexProps {
  projects: Project[]
}

export default function ProjectsIndex({ projects }: ProjectsIndexProps) {
  const { current_org } = usePage<SharedProps>().props

  if (!current_org) return null

  return (
    <DashboardLayout reserveBreadcrumbSpace>
      <div className="space-y-6">
        <div className="flex items-center justify-between">
          <h1 className="text-3xl font-bold tracking-tight">Projects</h1>
          <Link href={`/${current_org.slug}/projects/new`}>
            <Button>
              <Plus className="mr-2 h-4 w-4" />
              New Project
            </Button>
          </Link>
        </div>

        <div className="grid gap-6 md:grid-cols-2 lg:grid-cols-3">
          {projects.map((project) => (
            <Link
              key={project.id}
              href={`/${current_org.slug}/projects/${project.slug}`}
              className="block group"
            >
              <Card className="h-full hover:border-primary/50 transition-colors">
                <CardContent className="p-6">
                  <div className="flex items-start justify-between">
                    <div className="space-y-1">
                      <div className="flex items-center gap-2">
                        <Box className="h-5 w-5 text-muted-foreground" />
                        <h3 className="font-semibold group-hover:text-primary transition-colors">
                          {project.name}
                        </h3>
                      </div>
                      <p className="text-sm text-muted-foreground pl-7">
                        {project.platform || 'Platform not set'}
                      </p>
                    </div>
                  </div>

                  <div className="mt-8 pt-4 border-t flex items-center justify-between text-xs text-muted-foreground">
                    <div className="flex items-center gap-1">
                      <Hash className="h-3 w-3" />
                      <span>{project.issues_count || 0} issues</span>
                    </div>
                    <span>
                      Created {formatDistanceToNow(new Date(project.created_at))} ago
                    </span>
                  </div>
                </CardContent>
              </Card>
            </Link>
          ))}

          {projects.length === 0 && (
            <div className="col-span-full">
              <Card className="border-dashed">
                <CardContent className="flex flex-col items-center justify-center py-12 text-center">
                  <Box className="h-12 w-12 text-muted-foreground mb-4 opacity-50" />
                  <h3 className="text-lg font-medium">No projects yet</h3>
                  <p className="text-sm text-muted-foreground mb-4 max-w-sm">
                    Get started by creating your first project to track errors.
                  </p>
                  <Link href={`/${current_org.slug}/projects/new`}>
                    <Button>Create Project</Button>
                  </Link>
                </CardContent>
              </Card>
            </div>
          )}
        </div>
      </div>
    </DashboardLayout>
  )
}
