import { api, setCsrfToken } from '@/api/client';

export interface User {
  id: string;
  email: string;
  role: string;
  status: string;
  createdAt: string;
  csrfToken?: string;
}

/** Query fn for ['me']: session check + CSRF token hydration (plan §7). */
export async function fetchMe(): Promise<User> {
  const me = await api<User>('/auth/me');
  setCsrfToken(me.csrfToken ?? null);
  return me;
}

export function login(email: string, password: string): Promise<User> {
  return api<User>('/auth/login', { json: { email, password } });
}

export function logout(): Promise<void> {
  return api<void>('/auth/logout', { method: 'POST', json: {} });
}
