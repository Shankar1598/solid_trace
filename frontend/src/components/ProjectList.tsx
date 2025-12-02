import { useQuery } from '@tanstack/react-query';
import { Link, useParams } from 'react-router-dom';
import { api } from '@/lib/api';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Plus } from 'lucide-react';

export function ProjectList() {
  const { orgSlug } = useParams<{ orgSlug: string }>();

  const { data: projects, isLoading } = useQuery({
    queryKey: ['projects', orgSlug],
    queryFn: () => api.getProjects(orgSlug!),
    enabled: !!orgSlug,
  });

  if (isLoading) {
    return <div className="p-6">Loading projects...</div>;
  }

  return (
    <div className="container mx-auto p-6 max-w-6xl">
      <div className="mb-6 flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold">Projects</h1>
          <p className="text-muted-foreground">Manage your projects and their settings</p>
        </div>
        <Button>
          <Plus className="w-4 h-4 mr-2" />
          Create Project
        </Button>
      </div>

      <div className="grid gap-6 md:grid-cols-2 lg:grid-cols-3">
        {projects?.map((project) => (
          <Link
            key={project.id}
            to={`/${orgSlug}/projects/${project.slug}/settings`}
            className="block transition-transform hover:-translate-y-1"
          >
            <Card className="h-full hover:border-primary/50 transition-colors">
              <CardHeader>
                <div className="flex justify-between items-start">
                  <CardTitle className="text-xl">{project.name}</CardTitle>
                  <Badge variant="secondary">{project.platform || 'Unknown'}</Badge>
                </div>
                <CardDescription>
                  {project.slug}
                </CardDescription>
              </CardHeader>
              <CardContent>
                <div className="text-sm text-muted-foreground">
                  Created {new Date(project.created_at).toLocaleDateString()}
                </div>
              </CardContent>
            </Card>
          </Link>
        ))}
        {projects?.length === 0 && (
          <div className="col-span-full text-center py-12 text-muted-foreground bg-muted/10 rounded-lg border border-dashed">
            No projects found. Create one to get started.
          </div>
        )}
      </div>
    </div>
  );
}
