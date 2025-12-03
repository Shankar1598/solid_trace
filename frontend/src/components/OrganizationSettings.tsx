import { useState } from 'react';
import { useAuth } from '../context/AuthContext';
import { api } from '@/lib/api';
import { useParams } from 'react-router-dom';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Label } from '@/components/ui/label';
import { Input } from '@/components/ui/input';
import { Button } from '@/components/ui/button';

export function OrganizationSettings() {
  const { user } = useAuth();
  const { orgSlug } = useParams();

  if (!user) return null;

  const currentOrg = user.organizations.find(o => o.slug === orgSlug);

  if (!currentOrg) {
    return <div>Organization not found</div>;
  }

  return (
    <div className="space-y-6">
      <div>
        <h3 className="text-lg font-medium">Organization Settings</h3>
        <p className="text-sm text-muted-foreground">
          Manage your organization details and preferences.
        </p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>General Information</CardTitle>
          <CardDescription>
            Basic details about your organization.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="grid gap-2">
            <Label htmlFor="orgName">Organization Name</Label>
            <Input
              id="orgName"
              defaultValue={currentOrg.name}
              placeholder="e.g. Acme Corp"
            />
          </div>
          <div className="grid gap-2">
            <Label htmlFor="orgSlug">Organization Slug</Label>
            <Input
              id="orgSlug"
              defaultValue={currentOrg.slug}
              disabled
              className="bg-muted"
            />
            <p className="text-[0.8rem] text-muted-foreground">
              The slug is used in URLs and cannot be changed.
            </p>
          </div>
          <div className="flex justify-end">
            <Button>Save Changes</Button>
          </div>
        </CardContent>
      </Card>

      <MembersCard orgSlug={currentOrg.slug} />
    </div>
  );
}

function MembersCard({ orgSlug }: { orgSlug: string }) {
  const [email, setEmail] = useState('');
  const [isLoading, setIsLoading] = useState(false);
  const [status, setStatus] = useState<{ type: 'success' | 'error'; message: string } | null>(null);

  const handleInvite = async (e: React.FormEvent) => {
    e.preventDefault();
    setIsLoading(true);
    setStatus(null);

    try {
      await api.inviteUser(orgSlug, email);
      setStatus({ type: 'success', message: 'Invitation sent successfully.' });
      setEmail('');
    } catch (error: any) {
      setStatus({ type: 'error', message: error.response?.data?.error || 'Failed to send invitation.' });
    } finally {
      setIsLoading(false);
    }
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle>Members</CardTitle>
        <CardDescription>
          Invite new members to your organization.
        </CardDescription>
      </CardHeader>
      <CardContent>
        <form onSubmit={handleInvite} className="space-y-4">
          <div className="grid gap-2">
            <Label htmlFor="inviteEmail">Email Address</Label>
            <div className="flex gap-2">
              <Input
                id="inviteEmail"
                type="email"
                placeholder="colleague@example.com"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                required
              />
              <Button type="submit" disabled={isLoading}>
                {isLoading ? 'Sending...' : 'Invite'}
              </Button>
            </div>
          </div>
          {status && (
            <div className={`text-sm ${status.type === 'success' ? 'text-green-600' : 'text-red-600'}`}>
              {status.message}
            </div>
          )}
        </form>
      </CardContent>
    </Card>
  );
}
