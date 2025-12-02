import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Link, useParams } from 'react-router-dom';
import { api } from '@/lib/api';
import type { ProjectKey } from '@/lib/api';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';

export function ProjectSettings() {
  const { projectId } = useParams<{ projectId: string }>();
  const id = Number(projectId) || 1; // Default to 1 if not present
  const queryClient = useQueryClient();
  const [copiedId, setCopiedId] = useState<number | null>(null);

  const { data: keys, isLoading } = useQuery({
    queryKey: ['projectKeys', id],
    queryFn: () => api.getProjectKeys(id),
  });

  const createKeyMutation = useMutation({
    mutationFn: () => api.createProjectKey(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['projectKeys', id] });
    },
  });

  const rotateKeyMutation = useMutation({
    mutationFn: (keyId: number) => api.rotateProjectKey(keyId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['projectKeys', id] });
    },
  });

  const deleteKeyMutation = useMutation({
    mutationFn: (keyId: number) => api.deleteProjectKey(keyId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['projectKeys', id] });
    },
  });

  const copyToClipboard = (text: string, keyId: number) => {
    navigator.clipboard.writeText(text);
    setCopiedId(keyId);
    setTimeout(() => setCopiedId(null), 2000);
  };

  if (isLoading) {
    return <div className="p-6">Loading settings...</div>;
  }

  return (
    <div className="container mx-auto p-6 max-w-6xl">
      <div className="mb-6 flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold">Project Settings</h1>
          <p className="text-muted-foreground">Manage your project configuration and keys</p>
        </div>
        <Link to="/">
          <Button variant="outline">← Back to Issues</Button>
        </Link>
      </div>

      <div className="grid gap-6">
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

        <Card>
          <CardHeader>
            <CardTitle>General Settings</CardTitle>
            <CardDescription>Basic project configuration</CardDescription>
          </CardHeader>
          <CardContent>
            <div className="space-y-4">
              <div className="grid gap-2">
                <label className="text-sm font-medium">Project Name</label>
                <div className="p-2 border rounded-md bg-muted/50 text-muted-foreground">
                  Garnet Project
                </div>
                <p className="text-xs text-muted-foreground">
                  Project name cannot be changed in this version.
                </p>
              </div>

              <div className="grid gap-2">
                <label className="text-sm font-medium">Platform</label>
                <div className="flex gap-2">
                  <Badge variant="secondary">Ruby</Badge>
                  <Badge variant="secondary">JavaScript</Badge>
                  <Badge variant="secondary">Python</Badge>
                </div>
              </div>
              <div className="grid gap-2">
                <label className="text-sm font-medium">Debug</label>
                <div className="flex gap-2">
                  <Button
                    variant="destructive"
                    onClick={() => { throw new Error("This is a test error from the Garnet Frontend!"); }}
                  >
                    Trigger Frontend Error
                  </Button>
                </div>
              </div>
            </div>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
