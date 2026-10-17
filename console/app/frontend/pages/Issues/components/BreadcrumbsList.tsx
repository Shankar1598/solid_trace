import { format } from 'date-fns'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { MoreHorizontal, Terminal, Activity, MousePointer2 } from 'lucide-react'
import { BreadcrumbValue } from '@/types'

interface BreadcrumbsListProps {
  breadcrumbs: {
    values: BreadcrumbValue[]
  }
}

export function BreadcrumbsList({ breadcrumbs }: BreadcrumbsListProps) {
  if (!breadcrumbs?.values?.length) {
    return null
  }

  const getIcon = (category: string) => {
    if (category.startsWith('ui.')) return <MousePointer2 className="h-3.5 w-3.5" />
    if (category.startsWith('console')) return <Terminal className="h-3.5 w-3.5" />
    if (category.startsWith('sql')) return <Activity className="h-3.5 w-3.5" />
    return <MoreHorizontal className="h-3.5 w-3.5" />
  }

  const getLevelColor = (level: string | null) => {
    switch (level) {
      case 'fatal':
      case 'error':
        return 'text-destructive'
      case 'warning':
        return 'text-warning'
      case 'info':
        return 'text-info'
      default:
        return 'text-muted-foreground'
    }
  }

  return (
    <Card>
      <CardHeader className="border-b py-3">
        <CardTitle>Breadcrumbs</CardTitle>
      </CardHeader>
      <CardContent className="p-0">
        <div className="divide-y">
          {breadcrumbs.values.map((breadcrumb, index) => (
            <div key={index} className="flex items-start gap-4 p-3 text-sm hover:bg-muted/40">
              <div className="flex-none pt-1">
                <div className={`p-1.5 bg-muted ${getLevelColor(breadcrumb.level)}`}>
                  {getIcon(breadcrumb.category)}
                </div>
              </div>

              <div className="flex-1 min-w-0 space-y-1">
                <div className="flex items-center gap-2">
                  <span className="font-mono text-xs text-muted-foreground">
                    {breadcrumb.category}
                  </span>
                  {breadcrumb.level && (
                    <span className={`font-mono text-xs ${getLevelColor(breadcrumb.level)}`}>
                      {breadcrumb.level}
                    </span>
                  )}
                </div>

                <p className="font-mono text-xs break-all">
                  {breadcrumb.message || (breadcrumb.data && (
                    (typeof breadcrumb.data.message === 'string' && breadcrumb.data.message) ||
                    (typeof breadcrumb.data.sql === 'string' && breadcrumb.data.sql) ||
                    JSON.stringify(breadcrumb.data)
                  )) || <span className="italic text-muted-foreground">No message</span>}
                </p>
              </div>

              <div className="flex-none font-mono text-xs text-muted-foreground whitespace-nowrap">
                {format(new Date(breadcrumb.timestamp * 1000), 'HH:mm:ss.SSS')}
              </div>
            </div>
          ))}
        </div>
      </CardContent>
    </Card>
  )
}
