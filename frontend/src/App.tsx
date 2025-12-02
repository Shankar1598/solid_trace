import { Routes, Route, Navigate } from 'react-router-dom';
import { IssueList } from './components/IssueList';
import { IssueDetail } from './components/IssueDetail';
import { ProjectSettings } from './components/ProjectSettings';
import { Login } from './components/Login';
import { Register } from './components/Register';
import { useAuth } from './context/AuthContext';
import { ProtectedRoute } from './components/ProtectedRoute';
import { DashboardLayout } from './layouts/DashboardLayout';

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

        {/* Placeholder routes for now */}
        <Route path="/:orgSlug/projects" element={<div>Projects Page</div>} />
        <Route path="/:orgSlug/settings" element={<div>Organization Settings</div>} />

        <Route path="/:orgSlug/projects/:projectSlug/settings" element={<ProjectSettings />} />
      </Route>
    </Routes>
  );
}
