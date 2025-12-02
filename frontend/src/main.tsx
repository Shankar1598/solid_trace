import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import './index.css'
import { IssueList } from './components/IssueList'
import { IssueDetail } from './components/IssueDetail'
import { ProjectSettings } from './components/ProjectSettings'
import { Login } from './components/Login'
import { Register } from './components/Register'
import { AuthProvider, useAuth } from './context/AuthContext'
import { ProtectedRoute } from './components/ProtectedRoute'
import * as Sentry from "@sentry/react";

Sentry.init({
  dsn: "http://testkey123@localhost:3000/1",
  integrations: [
    Sentry.browserTracingIntegration(),
    Sentry.replayIntegration(),
  ],
  // Tracing
  tracesSampleRate: 1.0,
  // Session Replay
  replaysSessionSampleRate: 0.1,
  replaysOnErrorSampleRate: 1.0,
});

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      refetchOnWindowFocus: false,
      retry: 1,
    },
  },
});

function Home() {
  const { user } = useAuth();

  if (!user) return <div>Loading...</div>;

  if (user.organizations.length === 0) return <div>No organizations</div>;

  const firstOrg = user.organizations[0];

  return <Navigate to={`/${firstOrg.slug}/issues`} replace />;
}

function App() {
  return (
    <div className="min-h-screen bg-background">
      <main>
        <Routes>
          <Route path="/login" element={<Login />} />
          <Route path="/register" element={<Register />} />

          <Route path="/" element={<Navigate to="/home" replace />} />
          <Route path="/home" element={
            <ProtectedRoute>
              <Home />
            </ProtectedRoute>
          } />
          <Route path="/:orgSlug/issues" element={
            <ProtectedRoute>
              <IssueList />
            </ProtectedRoute>
          } />
          <Route path="/:orgSlug/issues/:issueId" element={
            <ProtectedRoute>
              <IssueDetail />
            </ProtectedRoute>
          } />
          <Route path="/:orgSlug/projects/:projectSlug/settings" element={
            <ProtectedRoute>
              <ProjectSettings />
            </ProtectedRoute>
          } />
        </Routes>
      </main>
    </div>
  );
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <AuthProvider>
        <BrowserRouter>
          <App />
        </BrowserRouter>
      </AuthProvider>
    </QueryClientProvider>
  </StrictMode>,
)
