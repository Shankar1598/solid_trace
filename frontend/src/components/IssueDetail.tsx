import { useQuery } from '@tanstack/react-query';
import { useParams, Link } from 'react-router-dom';
import { api } from '@/lib/api';
import { Badge } from '@/components/ui/badge';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Stacktrace } from './Stacktrace';
import { Breadcrumbs } from './Breadcrumbs';

const getLevelBadge = (level: number) => {
  switch (level) {
    case 50: return <Badge variant="destructive">Fatal</Badge>;
    case 40: return <Badge variant="destructive">Error</Badge>;
    case 30: return <Badge className="bg-yellow-500">Warning</Badge>;
    case 20: return <Badge className="bg-blue-500">Info</Badge>;
    case 10: return <Badge variant="outline">Debug</Badge>;
    default: return <Badge>Unknown</Badge>;
  }
};

const getStatusBadge = (status: number) => {
  return status === 0 ?
    <Badge variant="destructive">Unresolved</Badge> :
    <Badge variant="secondary">Resolved</Badge>;
};

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



  if (!orgSlug || !issueId) {

    return (

      <div className="flex items-center justify-center min-h-screen">

        <div className="text-lg text-red-500">No issue or org selected.</div>

      </div>

    );

  }



  if (isLoading) {
    return (
      <div className="flex items-center justify-center min-h-screen">
        <div className="text-lg">Loading...</div>
      </div>
    );
  }

  if (error || !issue) {
    return (
      <div className="flex items-center justify-center min-h-screen">
        <div className="text-lg text-red-500">Error loading issue</div>
      </div>
    );
  }

  return (
    <div className="container mx-auto p-6 max-w-6xl">
      <div className="mb-6">
        <Link to={`/${orgSlug}/issues`}>
          <Button variant="outline" size="sm">← Back to Issues</Button>
        </Link>
      </div>

      <Card>
        <CardHeader>
          <div className="flex items-start justify-between">
            <div className="space-y-2">
              <CardTitle className="text-2xl">{issue.title}</CardTitle>
              <CardDescription>Issue #{issue.id}</CardDescription>
            </div>
            <div className="flex gap-2 items-center">
              {issue.status === 0 ? (
                <Button onClick={handleResolve} variant="default" size="sm">
                  Resolve Issue
                </Button>
              ) : (
                <Button onClick={handleUnresolve} variant="outline" size="sm">
                  Unresolve
                </Button>
              )}
              {getLevelBadge(issue.level)}
              {getStatusBadge(issue.status)}
            </div>
          </div>
        </CardHeader>
        <CardContent>
          <div className="space-y-4">
            <div className="flex gap-8 text-sm text-muted-foreground">
              <div>
                <span className="font-medium">Events:</span> {issue.events.length}
              </div>
              <div>
                <span className="font-medium">First Seen:</span>{' '}
                {new Date(issue.created_at).toLocaleString()}
              </div>
              <div>
                <span className="font-medium">Last Seen:</span>{' '}
                {new Date(issue.updated_at).toLocaleString()}
              </div>
            </div>
          </div>
        </CardContent>
      </Card>

      <div className="mt-6 space-y-4">
        <Card>
          <CardHeader>
            <CardTitle>Events ({issue.events.length})</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="space-y-4">
              {issue.events.map((event) => (
                <Card key={event.id} className="bg-muted/50">
                  <CardHeader>
                    <div className="flex justify-between items-center">
                      <CardTitle className="text-sm">Event #{event.id}</CardTitle>
                      <span className="text-sm text-muted-foreground">
                        {new Date(event.created_at).toLocaleString()}
                      </span>
                    </div>
                  </CardHeader>
                  <CardContent className="space-y-4">
                    {/* Stacktrace */}
                    {event.data.exception && (
                      <Stacktrace exception={event.data.exception} />
                    )}

                    {/* Breadcrumbs */}
                    {event.data.breadcrumbs && (
                      <Breadcrumbs breadcrumbs={event.data.breadcrumbs} />
                    )}

                    {/* Tags */}
                    {event.data.tags && Object.keys(event.data.tags).length > 0 && (
                      <Card>
                        <CardHeader>
                          <CardTitle className="text-sm">Tags</CardTitle>
                        </CardHeader>
                        <CardContent>
                          <div className="flex flex-wrap gap-2">
                            {Object.entries(event.data.tags).map(([key, value]) => (
                              <Badge key={key} variant="outline" className="font-mono text-xs">
                                {key}: {String(value)}
                              </Badge>
                            ))}
                          </div>
                        </CardContent>
                      </Card>
                    )}

                    {/* Contexts (User, Request, etc) */}
                    {event.data.user && (
                      <Card>
                        <CardHeader>
                          <CardTitle className="text-sm">User</CardTitle>
                        </CardHeader>
                        <CardContent>
                          <dl className="grid grid-cols-[100px_1fr] gap-2 text-sm">
                            {Object.entries(event.data.user).map(([key, value]) => (
                              <div key={key} className="contents">
                                <dt className="text-muted-foreground font-medium">{key}</dt>
                                <dd className="font-mono">{String(value)}</dd>
                              </div>
                            ))}
                          </dl>
                        </CardContent>
                      </Card>
                    )}

                    {event.data.request && (
                      <Card>
                        <CardHeader>
                          <CardTitle className="text-sm">Request</CardTitle>
                        </CardHeader>
                        <CardContent>
                          <div className="space-y-4">
                            {event.data.request.url && (
                              <div className="grid grid-cols-[100px_1fr] gap-2 text-sm">
                                <div className="text-muted-foreground font-medium">URL</div>
                                <div className="font-mono break-all">{event.data.request.url}</div>
                              </div>
                            )}
                            {event.data.request.method && (
                              <div className="grid grid-cols-[100px_1fr] gap-2 text-sm">
                                <div className="text-muted-foreground font-medium">Method</div>
                                <div className="font-mono">{event.data.request.method}</div>
                              </div>
                            )}
                            {event.data.request.headers && (
                              <div>
                                <div className="text-muted-foreground font-medium text-sm mb-2">Headers</div>
                                <pre className="bg-muted/50 p-2 rounded text-xs overflow-auto">
                                  {JSON.stringify(event.data.request.headers, null, 2)}
                                </pre>
                              </div>
                            )}
                          </div>
                        </CardContent>
                      </Card>
                    )}

                    {/* Extra data */}
                    {event.data.extra && Object.keys(event.data.extra).length > 0 && (
                      <Card>
                        <CardHeader>
                          <CardTitle className="text-sm">Additional Data</CardTitle>
                        </CardHeader>
                        <CardContent>
                          <pre className="bg-background p-4 rounded-md overflow-auto text-xs">
                            {JSON.stringify(event.data.extra, null, 2)}
                          </pre>
                        </CardContent>
                      </Card>
                    )}

                    {/* Raw event data (collapsed by default) */}
                    <details>
                      <summary className="cursor-pointer text-sm text-muted-foreground hover:text-foreground">
                        View raw event data
                      </summary>
                      <pre className="bg-background p-4 rounded-md overflow-auto text-xs mt-2">
                        {JSON.stringify(event.data, null, 2)}
                      </pre>
                    </details>
                  </CardContent>
                </Card>
              ))}
            </div>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
