import { useState, useEffect } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Link, useParams } from 'react-router-dom';
import { api } from '@/lib/api';
import type { ProjectKey } from '@/lib/api';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';

export function ProjectSettings() {
  const { orgSlug, projectSlug } = useParams<{ orgSlug: string; projectSlug: string }>();
  const queryClient = useQueryClient();
  const [copiedId, setCopiedId] = useState<number | null>(null);
  const [projectName, setProjectName] = useState('');

  // Fetch Project Details
  const { data: project, isLoading: isProjectLoading } = useQuery({
    queryKey: ['project', orgSlug, projectSlug],
    queryFn: () => api.getProject(orgSlug!, projectSlug!),
    enabled: !!orgSlug && !!projectSlug,
  });

  // Fetch Project Keys
  const { data: keys, isLoading: isKeysLoading } = useQuery({
    queryKey: ['projectKeys', orgSlug, projectSlug],
    queryFn: () => api.getProjectKeys(orgSlug!, projectSlug!),
    enabled: !!orgSlug && !!projectSlug,
  });

  // Update local state when project data is loaded
  useEffect(() => {
    if (project) {
      setProjectName(project.name);
    }
  }, [project]);

  const updateProjectMutation = useMutation({
    mutationFn: (name: string) => api.updateProject(orgSlug!, projectSlug!, { name }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['project', orgSlug, projectSlug] });
      // toast({ title: "Project updated" });
    },
  });

  const createKeyMutation = useMutation({
    mutationFn: () => api.createProjectKey(orgSlug!, projectSlug!),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['projectKeys', orgSlug, projectSlug] });
    },
  });

  const rotateKeyMutation = useMutation({
    mutationFn: (keyId: number) => api.rotateProjectKey(orgSlug!, projectSlug!, keyId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['projectKeys', orgSlug, projectSlug] });
    },
  });

  const deleteKeyMutation = useMutation({
    mutationFn: (keyId: number) => api.deleteProjectKey(orgSlug!, projectSlug!, keyId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['projectKeys', orgSlug, projectSlug] });
    },
  });

  const copyToClipboard = (text: string, keyId: number) => {
    navigator.clipboard.writeText(text);
    setCopiedId(keyId);
    setTimeout(() => setCopiedId(null), 2000);
  };

  if (isProjectLoading || isKeysLoading) {
    return <div className="p-6">Loading settings...</div>;
  }

  if (!project) {
    return <div className="p-6">Project not found</div>;
  }

  return (
    <div className="container mx-auto p-6 max-w-6xl">
      <div className="mb-6 flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold">Project Settings</h1>
          <p className="text-muted-foreground">Manage your project configuration and keys</p>
        </div>
        <Link to={`/${orgSlug}/projects`}>
          <Button variant="outline">← Back to Projects</Button>
        </Link>
      </div>

      <div className="grid gap-6">
        <Card>
          <CardHeader>
            <CardTitle>General Settings</CardTitle>
            <CardDescription>Basic project configuration</CardDescription>
          </CardHeader>
          <CardContent>
            <div className="space-y-4">
              <div className="grid gap-2">
                <Label htmlFor="projectName">Project Name</Label>
                <div className="flex gap-2">
                  <Input
                    id="projectName"
                    value={projectName}
                    onChange={(e) => setProjectName(e.target.value)}
                  />
                  <Button
                    onClick={() => updateProjectMutation.mutate(projectName)}
                    disabled={updateProjectMutation.isPending || projectName === project.name}
                  >
                    Save
                  </Button>
                </div>
              </div>

              <div className="grid gap-2">
                <Label>Platform</Label>
                <div>
                  <Badge variant="secondary">{project.platform || 'Unknown'}</Badge>
                </div>
              </div>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <div className="flex items-center justify-between">
              <div>
                <CardTitle>Client Keys (DSN)</CardTitle>
                <CardDescription>
                  These keys are used to send data to Garnet. Keep the secret key private.
                </CardDescription>
              </div>
              <Button onClick={() => createKeyMutation.mutate()} disabled={createKeyMutation.isPending}>
                Create New Key
              </Button>
            </div>
          </CardHeader>
          <CardContent>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Public Key</TableHead>
                  <TableHead className="w-[50%]">DSN (Client Key)</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {keys?.map((key: ProjectKey) => (
                  <TableRow key={key.id}>
                    <TableCell className="font-mono text-sm">{key.public_key}</TableCell>
                    <TableCell>
                      <div className="flex items-center gap-2">
                        <code className="bg-muted px-2 py-1 rounded text-xs font-mono break-all">
                          {key.dsn}
                        </code>
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => copyToClipboard(key.dsn, key.id)}
                        >
                          {copiedId === key.id ? 'Copied!' : 'Copy'}
                        </Button>
                      </div>
                    </TableCell>
                    <TableCell className="text-right">
                      <div className="flex justify-end gap-2">
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={() => {
                            if (confirm('Are you sure you want to rotate this key? The old DSN will stop working.')) {
                              rotateKeyMutation.mutate(key.id);
                            }
                          }}
                        >
                          Rotate
                        </Button>
                        <Button
                          variant="destructive"
                          size="sm"
                          onClick={() => {
                            if (confirm('Are you sure you want to delete this key? This action cannot be undone.')) {
                              deleteKeyMutation.mutate(key.id);
                            }
                          }}
                        >
                          Delete
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
                {keys?.length === 0 && (
                  <TableRow>
                    <TableCell colSpan={3} className="text-center py-6 text-muted-foreground">
                      No keys found. Create one to start sending events.
                    </TableCell>
                  </TableRow>
                )}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
