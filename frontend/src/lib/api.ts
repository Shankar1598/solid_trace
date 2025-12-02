import axios from 'axios';

const API_BASE_URL = 'http://localhost:3000/api/v1';

export interface Issue {
  id: number;
  title: string;
  status: number;
  level: number;
  event_count: number;
  culprit?: string;
  created_at: string;
  updated_at: string;
}

export interface IssueEvent {
  id: number;
  data: Record<string, any>;
  created_at: string;
}

export interface IssueDetail extends Issue {
  project_id: number;
  events: IssueEvent[];
}

export interface Project {
  id: number;
  name: string;
  slug: string;
  platform?: string;
  organization_id: number;
  created_at: string;
}

export interface ProjectKey {
  id: number;
  public_key: string;
  dsn: string;
}

export interface User {
  id: number;
  name: string;
  email: string;
  organizations: { id: number; name: string; slug: string }[];
}

export interface LoginCredentials {
  email: string;
  password: string;
}

export interface RegisterCredentials {
  name: string;
  email: string;
  password: string;
}

export interface AuthResponse {
  token: string;
  user: User;
}

export const api = {
  getProjects: async (orgSlug: string): Promise<Project[]> => {
    const response = await axios.get(`${API_BASE_URL}/${orgSlug}/projects`);
    return response.data;
  },

  getProject: async (orgSlug: string, projectSlug: string): Promise<Project> => {
    const response = await axios.get(`${API_BASE_URL}/${orgSlug}/projects/${projectSlug}`);
    return response.data;
  },

  updateProject: async (orgSlug: string, projectSlug: string, data: Partial<Project>): Promise<Project> => {
    const response = await axios.put(`${API_BASE_URL}/${orgSlug}/projects/${projectSlug}`, { project: data });
    return response.data;
  },

  getProjectKeys: async (orgSlug: string, projectSlug: string): Promise<ProjectKey[]> => {
    const response = await axios.get(`${API_BASE_URL}/${orgSlug}/projects/${projectSlug}/keys`);
    return response.data;
  },

  createProjectKey: async (orgSlug: string, projectSlug: string): Promise<ProjectKey> => {
    const response = await axios.post(`${API_BASE_URL}/${orgSlug}/projects/${projectSlug}/keys`);
    return response.data;
  },

  rotateProjectKey: async (orgSlug: string, projectSlug: string, keyId: number): Promise<ProjectKey> => {
    const response = await axios.put(`${API_BASE_URL}/${orgSlug}/projects/${projectSlug}/keys/${keyId}`);
    return response.data;
  },

  deleteProjectKey: async (orgSlug: string, projectSlug: string, keyId: number): Promise<void> => {
    await axios.delete(`${API_BASE_URL}/${orgSlug}/projects/${projectSlug}/keys/${keyId}`);
  },

  getIssues: async (orgSlug: string, params?: {
    level?: number;
    status?: number;
    query?: string;
  }): Promise<Issue[]> => {
    const response = await axios.get(`${API_BASE_URL}/${orgSlug}/issues`, {
      params
    });
    return response.data;
  },

  getIssue: async (orgSlug: string, issueId: number): Promise<IssueDetail> => {
    const response = await axios.get(`${API_BASE_URL}/${orgSlug}/issues/${issueId}`);
    return response.data;
  },

  resolveIssue: async (orgSlug: string, issueId: number): Promise<void> => {
    await axios.patch(`${API_BASE_URL}/${orgSlug}/issues/${issueId}/resolve`);
  },

  unresolveIssue: async (orgSlug: string, issueId: number): Promise<void> => {
    await axios.patch(`${API_BASE_URL}/${orgSlug}/issues/${issueId}/unresolve`);
  },

  // Auth
  login: async (credentials: LoginCredentials): Promise<AuthResponse> => {
    const response = await axios.post(`${API_BASE_URL}/auth/login`, credentials);
    return response.data;
  },

  register: async (credentials: RegisterCredentials): Promise<AuthResponse> => {
    const response = await axios.post(`${API_BASE_URL}/auth/register`, { user: credentials });
    return response.data;
  },

  getMe: async (token: string): Promise<User> => {
    const response = await axios.get(`${API_BASE_URL}/auth/me`, {
      headers: { Authorization: `Bearer ${token}` }
    });
    return response.data.user;
  },
};

// Add interceptor to inject token
axios.interceptors.request.use((config) => {
  const token = localStorage.getItem('token');
  if (token && !config.headers.Authorization) {
    config.headers.Authorization = `Bearer ${token}`;
  }
  return config;
});
