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

export { ApiError };
