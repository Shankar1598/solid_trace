import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Link, useParams } from 'react-router-dom';
import { api } from '@/lib/api';
import { Badge } from '@/components/ui/badge';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { useDebounce } from '@/lib/hooks';
import { useAuth } from '@/context/AuthContext';

const getLevelBadge = (level: number) => {
  switch (level) {
    case 50: return <Badge variant="destructive">Fatal</Badge>;
    case 40: return <Badge variant="destructive">Error</Badge>;
    case 30: return <Badge className="bg-yellow-500 hover:bg-yellow-600">Warning</Badge>;
    case 20: return <Badge className="bg-blue-500 hover:bg-blue-600">Info</Badge>;
    case 10: return <Badge variant="outline">Debug</Badge>;
    default: return <Badge>Unknown</Badge>;
  }
};

const getStatusBadge = (status: number) => {
  return status === 0 ?
    <Badge variant="destructive">Unresolved</Badge> :
    <Badge variant="secondary">Resolved</Badge>;
};

export function IssueList() {
  const { orgSlug } = useParams<{ orgSlug: string }>();
  const { user } = useAuth();
  const org = user?.organizations.find(o => o.slug === orgSlug);

  const [search, setSearch] = useState('');
  const [status, setStatus] = useState<string>('all');
  const [level, setLevel] = useState<string>('all');

  const debouncedSearch = useDebounce(search, 300);

  const { data: issues, isLoading, error } = useQuery({
    queryKey: ['issues', orgSlug, debouncedSearch, status, level],
    queryFn: () => orgSlug ? api.getIssues(orgSlug, {
      query: debouncedSearch || undefined,
      status: status !== 'all' ? Number(status) : undefined,
      level: level !== 'all' ? Number(level) : undefined,
    }) : Promise.reject('No org'),
    enabled: !!orgSlug,
  });

  if (!orgSlug) {
    return (
      <div className="flex items-center justify-center min-h-screen">
        <div className="text-lg text-red-500">No org selected.</div>
      </div>
    );
  }

  if (isLoading) {
    return (
      <div className="flex items-center justify-center min-h-screen">
        <div className="text-lg">Loading issues...</div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex items-center justify-center min-h-screen">
        <div className="text-lg text-red-500">Error loading issues</div>
      </div>
    );
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-3xl font-bold">Issues</CardTitle>
        <CardDescription>
          Error tracking for Org: {org?.name} ({orgSlug})
        </CardDescription>
      </CardHeader>
      <CardContent>
        <div className="flex gap-4 mb-6">
          <div className="flex-1">
            <Input
              placeholder="Search issues..."
              value={search}
              onChange={(e) => setSearch(e.target.value)}
            />
          </div>
          <Select value={status} onValueChange={setStatus}>
            <SelectTrigger className="w-[180px]">
              <SelectValue placeholder="Status" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">All Statuses</SelectItem>
              <SelectItem value="0">Unresolved</SelectItem>
              <SelectItem value="1">Resolved</SelectItem>
            </SelectContent>
          </Select>
          <Select value={level} onValueChange={setLevel}>
            <SelectTrigger className="w-[180px]">
              <SelectValue placeholder="Level" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">All Levels</SelectItem>
              <SelectItem value="50">Fatal</SelectItem>
              <SelectItem value="40">Error</SelectItem>
              <SelectItem value="30">Warning</SelectItem>
              <SelectItem value="20">Info</SelectItem>
              <SelectItem value="10">Debug</SelectItem>
            </SelectContent>
          </Select>
        </div>

        {!issues || issues.length === 0 ? (
          <div className="text-center py-12 text-muted-foreground">
            <p className="text-lg">No issues found</p>
            <p className="text-sm mt-2">Try adjusting your filters or send a new error</p>
          </div>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-[60%]">Issue</TableHead>
                <TableHead>Level</TableHead>
                <TableHead>Status</TableHead>
                <TableHead className="text-right">Events</TableHead>
                <TableHead>Last Seen</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {issues.map((issue) => (
                <TableRow key={issue.id} className="hover:bg-muted/50">
                  <TableCell>
                    <Link
                      to={`/${orgSlug}/issues/${issue.id}`}
                      className="font-medium hover:underline"
                    >
                      {issue.title}
                    </Link>
                    <div className="text-sm text-muted-foreground">
                      #{issue.id}
                    </div>
                  </TableCell>
                  <TableCell>{getLevelBadge(issue.level)}</TableCell>
                  <TableCell>{getStatusBadge(issue.status)}</TableCell>
                  <TableCell className="text-right font-mono">
                    {issue.event_count}
                  </TableCell>
                  <TableCell className="text-sm text-muted-foreground">
                    {new Date(issue.updated_at).toLocaleString()}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  );
}
