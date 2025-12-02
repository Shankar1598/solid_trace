import { useQuery } from '@tanstack/react-query';
import { useParams, Link } from 'react-router-dom';
import { api } from '@/lib/api';
import { Button } from '@/components/ui/button';
import { Separator } from '@/components/ui/separator';
import { Stacktrace } from './Stacktrace';
import { Breadcrumbs } from './Breadcrumbs';
import {
  ArrowLeft,
  Clock,
  Users,
  Share2,
  Bookmark,
  Activity,
  Globe,
  Server
} from 'lucide-react';

const getLevelColor = (level: number) => {
  switch (level) {
    case 50: return 'text-red-500 border-red-500'; // Fatal
    case 40: return 'text-orange-500 border-orange-500'; // Error
    case 30: return 'text-yellow-500 border-yellow-500'; // Warning
    case 20: return 'text-blue-500 border-blue-500'; // Info
    case 10: return 'text-gray-400 border-gray-400'; // Debug
    default: return 'text-gray-300 border-gray-300';
  }
};

const getLevelLabel = (level: number) => {
  switch (level) {
    case 50: return 'fatal';
    case 40: return 'error';
    case 30: return 'warning';
    case 20: return 'info';
    case 10: return 'debug';
    default: return 'unknown';
  }
}

export function IssueDetail() {
  const { orgSlug, issueId } = useParams<{ orgSlug: string; issueId: string }>();

  const { data: issue, isLoading, error, refetch } = useQuery({
    queryKey: ['issue', orgSlug, issueId],
    queryFn: () => orgSlug ? api.getIssue(orgSlug, Number(issueId)) : Promise.reject('No org'),
    enabled: !!orgSlug && !!issueId,
  });

  const handleResolve = async () => {
    if (!orgSlug || !issueId) return;
    try {
      await api.resolveIssue(orgSlug, Number(issueId));
      refetch();
    } catch (err) {
      console.error('Failed to resolve issue:', err);
    }
  };

  const handleUnresolve = async () => {
    if (!orgSlug || !issueId) return;
    try {
      await api.unresolveIssue(orgSlug, Number(issueId));
      refetch();
    } catch (err) {
      console.error('Failed to unresolve issue:', err);
    }
  };

  if (!orgSlug || !issueId) return null;

  if (isLoading) {
    return <div className="p-8 text-center text-muted-foreground">Loading issue...</div>;
  }

  if (error || !issue) {
    return <div className="p-8 text-center text-red-500">Error loading issue</div>;
  }

  const latestEvent = issue.events[0]; // Assuming sorted by date desc, or just take first for now

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="space-y-4">
        <div className="flex items-center gap-2 text-sm text-muted-foreground">
          <Link to={`/${orgSlug}/issues`} className="hover:text-foreground flex items-center gap-1">
            <ArrowLeft className="w-4 h-4" /> Issues
          </Link>
          <span>/</span>
          <span className="font-mono text-xs">#{issue.id}</span>
        </div>

        <div className="flex justify-between items-start">
          <div className="space-y-1">
            <h1 className="text-2xl font-bold text-foreground flex items-center gap-3">
              <span className={`text-xs uppercase px-1.5 py-0.5 border rounded font-mono ${getLevelColor(issue.level)}`}>
                {getLevelLabel(issue.level)}
              </span>
              {issue.title}
            </h1>
            <p className="text-muted-foreground font-mono text-sm">
              {issue.culprit || 'unknown location'}
            </p>
          </div>
          <div className="flex items-center gap-2">
            <Button variant="outline" size="sm" className="gap-2">
              <Share2 className="w-4 h-4" /> Share
            </Button>
            <Button variant="outline" size="sm" className="gap-2">
              <Bookmark className="w-4 h-4" /> Bookmark
            </Button>
            {issue.status === 0 ? (
              <Button onClick={handleResolve} variant="default" size="sm" className="bg-green-600 hover:bg-green-700 text-white">
                Resolve
              </Button>
            ) : (
              <Button onClick={handleUnresolve} variant="outline" size="sm">
                Unresolve
              </Button>
            )}
          </div>
        </div>

        {/* Stats Bar */}
        <div className="flex items-center gap-8 py-3 border-y bg-card/50 px-4 rounded-sm text-sm">
          <div className="flex items-center gap-2">
            <Activity className="w-4 h-4 text-muted-foreground" />
            <span className="font-medium">{issue.events.length}</span>
            <span className="text-muted-foreground">Events</span>
          </div>
          <div className="flex items-center gap-2">
            <Users className="w-4 h-4 text-muted-foreground" />
            <span className="font-medium">1</span> {/* Placeholder for user count */}
            <span className="text-muted-foreground">Users</span>
          </div>
          <div className="flex items-center gap-2 ml-auto text-muted-foreground">
            <Clock className="w-4 h-4" />
            <span>First seen {new Date(issue.created_at).toLocaleDateString()}</span>
            <span>•</span>
            <span>Last seen {new Date(issue.updated_at).toLocaleDateString()}</span>
          </div>
        </div>
      </div>

      {/* Main Content Grid */}
      <div className="grid grid-cols-1 lg:grid-cols-3 gap-8">
        {/* Left Column: Stacktrace, Breadcrumbs */}
        <div className="lg:col-span-2 space-y-8">

          {/* Tags (Mobile/Top view) */}
          <div className="lg:hidden">
            {/* ... tags ... */}
          </div>

          {latestEvent && (
            <>
              {/* Stacktrace */}
              {latestEvent.data.exception && (
                <div className="space-y-3">
                  <h3 className="text-lg font-semibold flex items-center gap-2">
                    <Server className="w-4 h-4" /> Exception
                  </h3>
                  <div className="border rounded-md overflow-hidden">
                    <Stacktrace exception={latestEvent.data.exception} />
                  </div>
                </div>
              )}

              {/* Breadcrumbs */}
              {latestEvent.data.breadcrumbs && (
                <div className="space-y-3">
                  <h3 className="text-lg font-semibold flex items-center gap-2">
                    <Activity className="w-4 h-4" /> Breadcrumbs
                  </h3>
                  <div className="border rounded-md overflow-hidden bg-card">
                    <Breadcrumbs breadcrumbs={latestEvent.data.breadcrumbs} />
                  </div>
                </div>
              )}
            </>
          )}
        </div>

        {/* Right Column: Sidebar (Tags, Context) */}
        <div className="space-y-6">
          {latestEvent && (
            <>
              {/* Tags */}
              {latestEvent.data.tags && Object.keys(latestEvent.data.tags).length > 0 && (
                <div className="space-y-3">
                  <h3 className="text-sm font-semibold uppercase tracking-wider text-muted-foreground">Tags</h3>
                  <div className="space-y-2">
                    {Object.entries(latestEvent.data.tags).map(([key, value]) => (
                      <div key={key} className="flex justify-between items-center text-sm">
                        <span className="text-muted-foreground">{key}</span>
                        <span className="font-mono text-foreground bg-muted px-1.5 py-0.5 rounded text-xs truncate max-w-[150px]" title={String(value)}>
                          {String(value)}
                        </span>
                      </div>
                    ))}
                  </div>
                </div>
              )}

              <Separator />

              {/* User Context */}
              {latestEvent.data.user && (
                <div className="space-y-3">
                  <h3 className="text-sm font-semibold uppercase tracking-wider text-muted-foreground">User</h3>
                  <div className="space-y-2">
                    {Object.entries(latestEvent.data.user).map(([key, value]) => (
                      <div key={key} className="flex justify-between items-center text-sm">
                        <span className="text-muted-foreground">{key}</span>
                        <span className="font-mono text-foreground text-xs truncate max-w-[150px]">
                          {String(value)}
                        </span>
                      </div>
                    ))}
                  </div>
                </div>
              )}

              <Separator />

              {/* Request Context */}
              {latestEvent.data.request && (
                <div className="space-y-3">
                  <h3 className="text-sm font-semibold uppercase tracking-wider text-muted-foreground">Request</h3>
                  <div className="space-y-2">
                    {latestEvent.data.request.url && (
                      <div className="space-y-1">
                        <span className="text-xs text-muted-foreground">URL</span>
                        <div className="font-mono text-xs break-all bg-muted p-1 rounded">{latestEvent.data.request.url}</div>
                      </div>
                    )}
                    {latestEvent.data.request.method && (
                      <div className="flex justify-between items-center text-sm">
                        <span className="text-muted-foreground">Method</span>
                        <span className="font-mono text-foreground text-xs">{latestEvent.data.request.method}</span>
                      </div>
                    )}
                  </div>
                </div>
              )}

              <Separator />

              {/* Device/Browser (if available in tags or contexts) */}
              <div className="space-y-3">
                <h3 className="text-sm font-semibold uppercase tracking-wider text-muted-foreground">Device</h3>
                <div className="flex items-center gap-2 text-sm">
                  <Globe className="w-4 h-4 text-muted-foreground" />
                  <span>Browser</span>
                  <span className="ml-auto font-mono text-xs">Chrome 120.0</span>
                </div>
                <div className="flex items-center gap-2 text-sm">
                  <Server className="w-4 h-4 text-muted-foreground" />
                  <span>OS</span>
                  <span className="ml-auto font-mono text-xs">Mac OS X 10.15</span>
                </div>
              </div>

            </>
          )}
        </div>
      </div>
    </div>
  );
}
