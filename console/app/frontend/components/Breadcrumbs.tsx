import { Link } from '@inertiajs/react'
import { ChevronRight } from 'lucide-react'
import { Organization } from '@/types'

interface BreadcrumbsProps {
  organization: Organization
  project?: {
    name: string
    slug: string
  }
  issue?: {
    number: number
    title: string
  }
  rootLabel?: string
}

export default function Breadcrumbs({ organization, project, issue, rootLabel }: BreadcrumbsProps) {
  return (
    <nav className="flex items-center space-x-1 text-sm text-muted-foreground mb-4">
      <Link
        href={`/${organization.slug}/issues`}
        className="hover:text-foreground transition-colors"
      >
        {rootLabel || organization.name}
      </Link>

      {project && (
        <>
          <ChevronRight className="h-4 w-4" />
          <Link
            href={`/${organization.slug}/projects/${project.slug}`}
            className="hover:text-foreground transition-colors"
          >
            {project.name}
          </Link>
        </>
      )}

      {issue && (
        <>
          <ChevronRight className="h-4 w-4" />
          <span className="text-foreground font-medium truncate max-w-[300px]" title={issue.title}>
            #{issue.number}
          </span>
        </>
      )}
    </nav>
  )
}
