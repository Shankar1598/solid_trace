import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Link, useParams } from 'react-router-dom';
import { api } from '@/lib/api';
import { Checkbox } from '@/components/ui/checkbox';
import { Input } from '@/components/ui/input';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { useDebounce } from '@/lib/hooks';
import { Clock, BarChart2 } from 'lucide-react';
import { cn } from '@/lib/utils';

const getLevelColor = (level: number) => {
  switch (level) {
    case 50: return 'bg-red-500'; // Fatal
    case 40: return 'bg-orange-500'; // Error
    case 30: return 'bg-yellow-500'; // Warning
    case 20: return 'bg-blue-500'; // Info
    case 10: return 'bg-gray-400'; // Debug
    default: return 'bg-gray-300';
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

export function IssueList() {
  const { orgSlug } = useParams<{ orgSlug: string }>();

  const [search, setSearch] = useState('');
  const [status, setStatus] = useState<string>('0'); // Default to Unresolved
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

  if (!orgSlug) return null;

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold text-foreground">Issues</h1>
      </div>

      {/* Filters Bar */}
      <div className="flex gap-2 p-2 bg-card border rounded-md shadow-sm">
        <div className="flex-1">
          <Input
            placeholder="Search by title, message, or tags"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="border-none shadow-none focus-visible:ring-0 bg-transparent"
          />
        </div>
        <div className="flex items-center gap-2 border-l pl-2">
          <Select value={status} onValueChange={setStatus}>
            <SelectTrigger className="w-[130px] border-none shadow-none focus:ring-0 h-8">
              <SelectValue placeholder="Status" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">All Statuses</SelectItem>
              <SelectItem value="0">Unresolved</SelectItem>
              <SelectItem value="1">Resolved</SelectItem>
            </SelectContent>
          </Select>
          <Select value={level} onValueChange={setLevel}>
            <SelectTrigger className="w-[110px] border-none shadow-none focus:ring-0 h-8">
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
      </div>

      {/* Issue List */}
      <div className="bg-card border rounded-md shadow-sm overflow-hidden">
        <div className="grid grid-cols-12 gap-4 p-3 border-b bg-muted/30 text-xs font-medium text-muted-foreground uppercase tracking-wider">
          <div className="col-span-6 pl-2">Issue</div>
          <div className="col-span-2 text-center">Graph</div>
          <div className="col-span-1 text-right">Events</div>
          <div className="col-span-1 text-right">Users</div>
          <div className="col-span-2 text-right pr-2">Last Seen</div>
        </div>

        {isLoading ? (
          <div className="p-8 text-center text-muted-foreground">Loading...</div>
        ) : error ? (
          <div className="p-8 text-center text-red-500">Error loading issues</div>
        ) : !issues || issues.length === 0 ? (
          <div className="p-12 text-center text-muted-foreground">
            <div className="flex justify-center mb-4">
              <div className="w-12 h-12 rounded-full bg-muted flex items-center justify-center">
                <CheckIcon className="w-6 h-6" />
              </div>
            </div>
            <h3 className="text-lg font-medium text-foreground">No issues found</h3>
            <p className="mt-1">You're doing great!</p>
          </div>
        ) : (
          <div className="divide-y">
            {issues.map((issue) => (
              <div key={issue.id} className="group grid grid-cols-12 gap-4 p-3 items-center hover:bg-muted/30 transition-colors">
                <div className="col-span-6 flex items-start gap-3 pl-2">
                  <Checkbox className="mt-1" />
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2 mb-0.5">
                      <span className={cn("w-2 h-2 rounded-sm", getLevelColor(issue.level))} title={getLevelLabel(issue.level)} />
                      <Link to={`/${orgSlug}/issues/${issue.id}`} className="font-medium text-primary hover:underline truncate block">
                        {issue.title}
                      </Link>
                    </div>
                    <div className="text-sm text-muted-foreground truncate font-mono">
                      {/* Placeholder for culprit/file location */}
                      {issue.culprit || 'unknown location'}
                    </div>
                  </div>
                </div>

                <div className="col-span-2 flex justify-center opacity-50">
                  {/* Sparkline Placeholder */}
                  <BarChart2 className="w-8 h-4 text-muted-foreground/50" />
                </div>

                <div className="col-span-1 text-right text-sm text-muted-foreground">
                  {issue.event_count}
                </div>

                <div className="col-span-1 text-right text-sm text-muted-foreground">
                  0
                </div>

                <div className="col-span-2 text-right text-sm text-muted-foreground pr-2 flex items-center justify-end gap-1">
                  <Clock className="w-3 h-3" />
                  {new Date(issue.updated_at).toLocaleDateString()}
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

function CheckIcon(props: any) {
  return (
    <svg
      {...props}
      xmlns="http://www.w3.org/2000/svg"
      width="24"
      height="24"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <polyline points="20 6 9 17 4 12" />
    </svg>
  )
}
