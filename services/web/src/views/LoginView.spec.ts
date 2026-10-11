import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils';
import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query';
import { defineComponent, h } from 'vue';
import { createMemoryHistory, createRouter } from 'vue-router';
import LoginView from './LoginView.vue';
import { getCsrfToken, setCsrfToken } from '@/api/client';
import { queryClient } from '@/api/queryClient';

const USER = {
  id: 'u1',
  email: 'lin@t.io',
  role: 'user',
  status: 'active',
  createdAt: '2026-01-01T00:00:00Z',
  csrfToken: 'tok-123',
};

function jsonResponse(body: unknown, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    statusText: status === 401 ? 'Unauthorized' : 'OK',
    json: async () => body,
  };
}

function freshClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
}

// vue-query's notifyManager schedules mutations on a macrotask.
async function settle() {
  await flushPromises();
  await new Promise((r) => setTimeout(r, 5));
  await flushPromises();
}

enableAutoUnmount(afterEach);

async function mountLogin(opts: { path?: string } = {}) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', name: 'home', component: defineComponent({ render: () => h('div') }) },
      { path: '/projects/:id', component: defineComponent({ render: () => h('div') }) },
      { path: '/login', component: LoginView },
      { path: '/register', component: defineComponent({ render: () => h('div') }) },
    ],
  });
  await router.push(opts.path ?? '/login');
  await router.isReady();

  const wrapper = mount(LoginView, {
    global: {
      plugins: [router, [VueQueryPlugin, { queryClient: freshClient() }]],
    },
  });
  return { wrapper, router };
}

const fetchMock = vi.fn();

beforeEach(() => {
  vi.stubGlobal('fetch', fetchMock);
  fetchMock.mockReset();
  setCsrfToken(null);
  queryClient.clear();
});

describe('login flow (plan §9)', () => {
  it('submits credentials, stores the session user + CSRF token, redirects', async () => {
    fetchMock.mockResolvedValue(jsonResponse(USER));
    const { wrapper, router } = await mountLogin();

    await wrapper.find('#email').setValue('lin@t.io');
    await wrapper.find('#password').setValue('correct horse');
    await wrapper.find('form').trigger('submit');
    await settle();

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, init] = fetchMock.mock.calls[0]!;
    expect(url).toBe('/api/auth/login');
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body)).toEqual({ email: 'lin@t.io', password: 'correct horse' });

    expect(queryClient.getQueryData(['me'])).toMatchObject({ email: 'lin@t.io', role: 'user' });
    expect(getCsrfToken()).toBe('tok-123'); // double-submit token for later calls
    expect(router.currentRoute.value.path).toBe('/');
  });

  it('honours the ?redirect= target after signing in', async () => {
    fetchMock.mockResolvedValue(jsonResponse(USER));
    const { wrapper, router } = await mountLogin({ path: '/login?redirect=%2Fprojects%2Fabc' });

    await wrapper.find('#email').setValue('lin@t.io');
    await wrapper.find('#password').setValue('correct horse');
    await wrapper.find('form').trigger('submit');
    await settle();

    expect(router.currentRoute.value.path).toBe('/projects/abc');
  });

  it('shows the server message on rejected credentials and stays put', async () => {
    fetchMock.mockResolvedValue(jsonResponse({ message: 'invalid credentials' }, 401));
    const { wrapper, router } = await mountLogin();

    await wrapper.find('#email').setValue('lin@t.io');
    await wrapper.find('#password').setValue('wrong');
    await wrapper.find('form').trigger('submit');
    await settle();

    expect(wrapper.find('[role="alert"]').text()).toContain('invalid credentials');
    expect(queryClient.getQueryData(['me'])).toBeUndefined();
    expect(router.currentRoute.value.path).toBe('/login');
  });

  it('client-side validation blocks the request entirely', async () => {
    const { wrapper } = await mountLogin();

    await wrapper.find('form').trigger('submit');
    await settle();

    expect(fetchMock).not.toHaveBeenCalled();
    expect(wrapper.text()).toContain('Required');
    expect(wrapper.find('[role="alert"]').exists()).toBe(false);
  });
});
