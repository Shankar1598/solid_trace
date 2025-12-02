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
    <div className="bg-card border rounded-md overflow-hidden">
      <div className="divide-y">
        {breadcrumbs.values.map((crumb, index) => (
          <div key={index} className="flex gap-4 p-3 text-sm hover:bg-muted/30 transition-colors">
            <div className="min-w-[80px] text-muted-foreground text-xs font-mono pt-0.5">
              {crumb.timestamp ? new Date(crumb.timestamp).toLocaleTimeString([], { hour12: false }) : '--:--:--'}
            </div>
            <div className="flex-1 space-y-1">
              <div className="flex items-center gap-2">
                <span className="font-semibold text-foreground">{crumb.category || crumb.type || 'default'}</span>
                {crumb.level && (
                  <Badge variant="outline" className="text-[10px] px-1.5 py-0 h-4 uppercase tracking-wider">
                    {crumb.level}
                  </Badge>
                )}
              </div>
              <div className="text-muted-foreground">
                {crumb.message}
              </div>
              {crumb.data && Object.keys(crumb.data).length > 0 && (
                <pre className="bg-muted/50 p-2 rounded text-xs mt-2 overflow-auto font-mono">
                  {JSON.stringify(crumb.data, null, 2)}
                </pre>
              )}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
