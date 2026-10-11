import { api, ApiError } from '@/api/client';

export interface Project {
  id: string;
  name: string;
  slug: string;
  type: string;
  wantsDb: boolean;
  desiredState: string;
  status: 'new' | 'deploying' | 'running' | 'stopped' | 'failed';
  currentDeploymentId: string | null;
  createdAt: string;
  url: string;
}

export function getProject(id: string): Promise<Project> {
  return api<Project>(`/projects/${id}`);
}

export type ActionType = 'stop' | 'start' | 'restart' | 'redeploy';

export function projectAction(id: string, type: ActionType): Promise<unknown> {
  return api(`/projects/${id}/${type}`, { json: {} });
}

export function uploadDeployment(id: string, file: File): Promise<unknown> {
  const form = new FormData();
  form.append('file', file);
  return api(`/projects/${id}/deployments`, { formData: form });
}

/** Statuses worth polling — plan §8: the worker transitions them asynchronously. */
export function isTransientStatus(status: Project['status'] | undefined): boolean {
  return status === 'new' || status === 'deploying';
}

// --- environment (M6.5) ----------------------------------------------------

export interface EnvVar {
  key: string;
  value: string;
  updatedAt: string;
}

export function listEnv(projectId: string): Promise<{ items: EnvVar[]; total: number }> {
  return api(`/projects/${projectId}/env`);
}

export function putEnv(projectId: string, key: string, value: string): Promise<unknown> {
  return api(`/projects/${projectId}/env/${encodeURIComponent(key)}`, { json: { value } });
}

export function deleteEnv(projectId: string, key: string): Promise<void> {
  return api(`/projects/${projectId}/env/${encodeURIComponent(key)}`, { method: 'DELETE' });
}

/** One-shot atomic bulk apply — server validates all keys, checks quota
 *  against the final state and writes everything in one transaction. */
export function putEnvBulk(
  projectId: string,
  input: { set?: Record<string, string>; remove?: string[] },
): Promise<{ saved: number; deleted: number }> {
  return api(`/projects/${projectId}/env`, { method: 'PUT', json: input });
}

// --- saved sources (M6.5) --------------------------------------------------

export interface Source {
  id: string;
  projectId: string;
  sizeBytes: number;
  sha256: string;
  originalFilename: string;
  createdAt: string;
  deletedAt: string | null;
}

export function listSources(projectId: string): Promise<{ items: Source[]; total: number }> {
  return api(`/projects/${projectId}/sources`);
}

// --- database status (M6.5) ------------------------------------------------

export interface ProjectDatabase {
  status: 'provisioning' | 'ready';
  dbName: string;
  dbUser: string;
  createdAt: string;
}

export function getDatabase(projectId: string): Promise<ProjectDatabase> {
  return api(`/projects/${projectId}/database`);
}

export { ApiError };
