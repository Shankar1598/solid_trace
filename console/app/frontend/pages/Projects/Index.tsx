import { Link } from '@inertiajs/react'
import DashboardLayout from '@/components/layouts/DashboardLayout'
import { PageHeader } from '@/components/PageHeader'
import { Card, CardContent } from '@/components/ui/card'
import { buttonVariants } from '@/components/ui/button'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Plus, Box } from 'lucide-react'
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
    <DashboardLayout>
      <PageHeader
        title="Projects"
        actions={
          <Link href={`/${current_org.slug}/projects/new`} className={buttonVariants()}>
            <Plus className="h-4 w-4" />
            New project
          </Link>
        }
      />

      {projects.length === 0 ? (
        <Card className="border-dashed">
          <CardContent className="flex flex-col items-center justify-center py-12 text-center">
            <Box className="h-12 w-12 text-muted-foreground mb-4 opacity-50" />
            <h3 className="text-lg font-medium">No projects yet</h3>
            <p className="text-sm text-muted-foreground mb-4 max-w-sm">
              Get started by creating your first project to track errors.
            </p>
            <Link href={`/${current_org.slug}/projects/new`} className={buttonVariants()}>Create project</Link>
          </CardContent>
        </Card>
      ) : (
        <div className="border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Platform</TableHead>
                <TableHead className="text-right">Issues</TableHead>
                <TableHead>Created</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {projects.map((project) => (
                <TableRow key={project.id}>
                  <TableCell>
                    <Link href={`/${current_org.slug}/projects/${project.slug}`} className="font-medium hover:underline">{project.name}</Link>
                    <div className="font-mono text-xs text-muted-foreground">{project.slug}</div>
                  </TableCell>
                  <TableCell className="font-mono text-xs">{project.platform || <span className="font-sans text-muted-foreground">Not set</span>}</TableCell>
                  <TableCell className="text-right font-mono text-xs">{(project.issues_count || 0).toLocaleString()}</TableCell>
                  <TableCell className="text-muted-foreground">{formatDistanceToNow(new Date(project.created_at))} ago</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </DashboardLayout>
  )
}
