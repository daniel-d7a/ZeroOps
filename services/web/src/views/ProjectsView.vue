<script setup lang="ts">
import { computed, ref } from 'vue';
import { useRouter } from 'vue-router';
import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query';
import { useField, useForm } from 'vee-validate';
import { toTypedSchema } from '@vee-validate/zod';
import * as z from 'zod';
import { api, ApiError } from '@/api/client';
import { fetchMe } from '@/api/auth';
import type { Project } from '@/api/projects';
import Badge from '@/components/ui/Badge.vue';
import Button from '@/components/ui/Button.vue';
import Card from '@/components/ui/Card.vue';
import Input from '@/components/ui/Input.vue';
import Label from '@/components/ui/Label.vue';

const router = useRouter();
const queryClient = useQueryClient();

const me = useQuery({ queryKey: ['me'], queryFn: fetchMe });
const projects = useQuery({
  queryKey: ['projects'],
  queryFn: () => api<{ items: Project[]; total: number }>('/projects'),
});

async function logout() {
  await api<void>('/auth/logout', { method: 'POST', json: {} });
  queryClient.setQueryData(['me'], null);
  void router.replace('/login');
}

// --- new project form (inline panel — a modal needs reka-ui; add one when
// a second dialog exists to share it) ---
const showNew = ref(false);
const serverError = ref('');
const createError = computed(() => serverError.value);

const schema = toTypedSchema(
  z.object({
    name: z.string().min(1, 'Name is required').max(100, 'At most 100 characters'),
    wantsDb: z.boolean(),
  }),
);
const { handleSubmit, errors, resetForm } = useForm({
  validationSchema: schema,
  initialValues: { name: '', wantsDb: false },
});
const name = useField<string>('name');
const wantsDb = useField<boolean>('wantsDb');

const createProject = useMutation({
  mutationFn: (input: { name: string; wantsDb: boolean }) => api<Project>('/projects', { json: input }),
  onSuccess: () => {
    showNew.value = false;
    resetForm();
    void queryClient.invalidateQueries({ queryKey: ['projects'] });
  },
  onError: (e) => {
    serverError.value = e instanceof ApiError ? e.message : 'could not create project';
  },
});

const submitCreate = handleSubmit((values) => {
  serverError.value = '';
  createProject.mutate({ name: values.name, wantsDb: values.wantsDb });
});

const statusClass: Record<string, string> = {
  running: 'bg-emerald-600 text-white border-transparent',
  deploying: 'bg-amber-500 text-white border-transparent',
  queued: 'bg-amber-500 text-white border-transparent',
  failed: 'bg-destructive text-destructive-foreground border-transparent',
};

function badgeClass(status: string) {
  return statusClass[status] ?? '';
}
</script>

<template>
  <main class="mx-auto mt-10 w-full max-w-4xl px-4">
    <header class="mb-6 flex items-baseline gap-3">
      <h1 class="text-xl font-semibold">Projects</h1>
      <span class="flex-1" />
      <span class="text-sm text-muted-foreground">{{ me.data.value?.email }}</span>
      <RouterLink v-if="me.data.value?.role === 'admin'" to="/admin">
        <Button variant="ghost" size="sm">Admin</Button>
      </RouterLink>
      <Button variant="outline" size="sm" @click="logout">Sign out</Button>
    </header>

    <div class="mb-4 flex items-center gap-3">
      <Button @click="showNew = !showNew">{{ showNew ? 'Cancel' : 'New project' }}</Button>
      <span class="text-sm text-muted-foreground">{{ projects.data.value?.total ?? 0 }} total</span>
    </div>

    <Card v-if="showNew" class="mb-6 p-4">
      <form class="flex flex-col gap-4" novalidate @submit="submitCreate">
        <div class="flex flex-col gap-2">
          <Label for="name">Name</Label>
          <Input id="name" placeholder="My First App" v-bind="name" />
          <p v-if="errors.name" class="text-sm text-destructive">{{ errors.name }}</p>
        </div>
        <label class="flex items-center gap-2 text-sm">
          <input
            type="checkbox"
            class="h-4 w-4 accent-primary"
            :checked="wantsDb.value.value"
            @change="wantsDb.handleChange($event)"
            @blur="wantsDb.handleBlur"
          />
          Needs a Postgres database
        </label>
        <p v-if="createError" class="text-sm text-destructive" role="alert">{{ createError }}</p>
        <div>
          <Button type="submit" :disabled="createProject.isPending.value">
            {{ createProject.isPending.value ? 'Creating…' : 'Create' }}
          </Button>
        </div>
      </form>
    </Card>

    <p v-if="projects.isPending.value" class="text-sm text-muted-foreground">Loading…</p>
    <p v-else-if="projects.data.value?.items.length === 0" class="text-sm text-muted-foreground">
      No projects yet — create one above.
    </p>
    <ul v-else class="flex flex-col gap-3">
      <li v-for="p in projects.data.value?.items" :key="p.id">
        <Card class="flex items-center gap-3 p-4">
          <RouterLink class="font-medium hover:underline" :to="`/projects/${p.id}`">{{ p.name }}</RouterLink>
          <Badge :class="badgeClass(p.status)">{{ p.status }}</Badge>
          <span v-if="p.wantsDb" class="text-xs text-muted-foreground">db</span>
          <span class="flex-1" />
          <a :href="p.url" class="text-sm text-muted-foreground underline" target="_blank" rel="noreferrer">
            {{ p.url }}
          </a>
        </Card>
      </li>
    </ul>
  </main>
</template>
