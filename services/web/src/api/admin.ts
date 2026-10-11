import { api } from '@/api/client';
import type { User } from '@/api/auth';

export interface AdminUser extends User {
  projectCount: number;
}

export interface AdminProject {
  id: string;
  name: string;
  slug: string;
  type: string;
  wantsDb: boolean;
  desiredState: string;
  status: string;
  currentDeploymentId: string | null;
  createdAt: string;
  ownerEmail: string;
  node: string | null; // worker that built the current deployment
  url: string;
}

export interface WorkerInfo {
  id: string; // hostname
  roles: string[];
  nodeId: string;
  startedAt: string;
  lastSeenAt: string;
}

export interface SystemInfo {
  workers: WorkerInfo[];
  deployments: { status: string; count: number }[];
}

const PAGE = 20;

export function listAdminUsers(offset: number): Promise<{ items: AdminUser[]; total: number }> {
  return api(`/admin/users?limit=${PAGE}&offset=${offset}`);
}

export function listAdminProjects(offset: number): Promise<{ items: AdminProject[]; total: number }> {
  return api(`/admin/projects?limit=${PAGE}&offset=${offset}`);
}

export function getSystem(): Promise<SystemInfo> {
  return api('/admin/system');
}

export function setUserStatus(id: string, status: 'active' | 'disabled'): Promise<User> {
  return api(`/admin/users/${id}`, { method: 'PATCH', json: { status } });
}

export function adminStop(projectId: string): Promise<unknown> {
  return api(`/admin/projects/${projectId}/stop`, { json: {} });
}
