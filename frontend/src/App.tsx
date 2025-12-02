import { Routes, Route, Navigate } from 'react-router-dom';
import { IssueList } from './components/IssueList';
import { IssueDetail } from './components/IssueDetail';
import { ProjectSettings } from './components/ProjectSettings';
import { Login } from './components/Login';
import { Register } from './components/Register';
import { useAuth } from './context/AuthContext';
import { ProtectedRoute } from './components/ProtectedRoute';
import { DashboardLayout } from './layouts/DashboardLayout';
import { SettingsLayout } from './layouts/SettingsLayout';
import { OrganizationSettings } from './components/OrganizationSettings';
import { UserSettings } from './components/UserSettings';
import { ProjectList } from './components/ProjectList';

function Home() {
  const { user } = useAuth();

  if (!user) return <div>Loading...</div>;
  if (user.organizations.length === 0) return <div>No organizations</div>;

  const firstOrg = user.organizations[0];
  return <Navigate to={`/${firstOrg.slug}/issues`} replace />;
}

export default function App() {
  return (
    <Routes>
      <Route path="/login" element={<Login />} />
      <Route path="/register" element={<Register />} />

      {/* Protected Routes wrapped in DashboardLayout */}
      <Route element={
        <ProtectedRoute>
          <DashboardLayout />
        </ProtectedRoute>
      }>
        <Route path="/" element={<Navigate to="/home" replace />} />
        <Route path="/home" element={<Home />} />

        <Route path="/:orgSlug/issues" element={<IssueList />} />
        <Route path="/:orgSlug/issues/:issueId" element={<IssueDetail />} />

        {/* Projects Routes */}
        <Route path="/:orgSlug/projects" element={<ProjectList />} />

        <Route path="/:orgSlug/settings" element={<SettingsLayout />}>
          <Route index element={<Navigate to="organization" replace />} />
          <Route path="organization" element={<OrganizationSettings />} />
          <Route path="user" element={<UserSettings />} />
        </Route>

        <Route path="/:orgSlug/projects/:projectSlug/settings" element={<ProjectSettings />} />
      </Route>
    </Routes>
  );
}
