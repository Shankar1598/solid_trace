import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';

interface Breadcrumb {
  category?: string;
  message?: string;
  level?: string;
  timestamp?: string;
  type?: string;
  data?: Record<string, any>;
}

interface BreadcrumbsProps {
  breadcrumbs?: {
    values?: Breadcrumb[];
  };
}

export function Breadcrumbs({ breadcrumbs }: BreadcrumbsProps) {
  if (!breadcrumbs?.values || breadcrumbs.values.length === 0) {
    return null;
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-lg">Breadcrumbs</CardTitle>
      </CardHeader>
      <CardContent>
        <div className="space-y-4">
          {breadcrumbs.values.map((crumb, index) => (
            <div key={index} className="flex gap-4 items-start text-sm">
              <div className="min-w-[140px] text-muted-foreground text-xs pt-1">
                {crumb.timestamp ? new Date(crumb.timestamp).toLocaleTimeString() : 'Unknown time'}
              </div>
              <div className="flex-1 space-y-1">
                <div className="flex items-center gap-2">
                  <span className="font-semibold">{crumb.category || crumb.type || 'default'}</span>
                  {crumb.level && (
                    <Badge variant="outline" className="text-xs py-0 h-5">
                      {crumb.level}
                    </Badge>
                  )}
                </div>
                <div className="text-muted-foreground">
                  {crumb.message}
                </div>
                {crumb.data && Object.keys(crumb.data).length > 0 && (
                  <pre className="bg-muted/50 p-2 rounded text-xs mt-1 overflow-auto">
                    {JSON.stringify(crumb.data, null, 2)}
                  </pre>
                )}
              </div>
            </div>
          ))}
        </div>
      </CardContent>
    </Card>
  );
}