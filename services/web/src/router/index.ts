import { createRouter, createWebHistory } from 'vue-router';
import { queryClient } from '@/api/queryClient';
import { fetchMe, type User } from '@/api/auth';

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', name: 'projects', component: () => import('@/views/ProjectsView.vue'), meta: { requiresAuth: true } },
    { path: '/projects/:id', name: 'project', component: () => import('@/views/ProjectDetailView.vue'), meta: { requiresAuth: true } },
    { path: '/login', name: 'login', component: () => import('@/views/LoginView.vue') },
    { path: '/register', name: 'register', component: () => import('@/views/RegisterView.vue') },
    { path: '/admin', name: 'admin', component: () => import('@/views/AdminView.vue'), meta: { requiresAuth: true, requiresAdmin: true } },
    // SPA fallback also covers unknown paths (Caddy serves index.html).
    { path: '/:pathMatch(.*)*', redirect: '/' },
  ],
});

router.beforeEach(async (to) => {
  // Session check through the query cache: first nav fetches, later navs hit
  // the cache (staleTime in queryClient). 401 -> null = logged out.
  let me = queryClient.getQueryData<User>(['me']) ?? null;
  if (me === null && !queryClient.getQueryState(['me'])?.error) {
    me = await queryClient.query({ queryKey: ['me'], queryFn: fetchMe }).catch(() => null);
  }
  if (to.meta.requiresAuth && !me) {
    return { path: '/login', query: { redirect: to.fullPath } };
  }
  // Client-side role gate (plan §M6.6); the API's RolesGuard is the real
  // enforcement — this just avoids rendering a page that would 403.
  if (to.meta.requiresAdmin && me?.role !== 'admin') return '/';
  if (to.path === '/login' && me) return '/';
});
