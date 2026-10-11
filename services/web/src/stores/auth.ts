import { defineStore } from 'pinia';
import { api, setCsrfToken } from '@/api/client';

export interface User {
  id: string;
  email: string;
  role: string;
  status: string;
  createdAt: string;
  csrfToken?: string;
}

// Session state for the whole console. The CSRF token cannot be read from
// the cookie (HttpOnly) — login and /auth/me both return it in the body and
// the client replays it in X-CSRF-Token on every state-changing request.
export const useAuthStore = defineStore('auth', {
  state: () => ({
    user: null as User | null,
    loaded: false, // fetchMe finished — the router guard waits on this
  }),
  actions: {
    async fetchMe() {
      try {
        const me = await api<User>('/auth/me');
        this.user = me;
        setCsrfToken(me.csrfToken ?? null);
      } catch {
        this.user = null;
        setCsrfToken(null);
      } finally {
        this.loaded = true;
      }
    },
    async login(email: string, password: string) {
      const res = await api<User>('/auth/login', { json: { email, password } });
      this.user = res;
      this.loaded = true;
      setCsrfToken(res.csrfToken ?? null);
    },
    async logout() {
      await api<void>('/auth/logout', { method: 'POST', json: {} });
      this.user = null;
      setCsrfToken(null);
    },
  },
});
