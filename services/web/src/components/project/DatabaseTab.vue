<script setup lang="ts">
import { computed } from 'vue';
import { useQuery } from '@tanstack/vue-query';
import { ApiError } from '@/api/client';
import { getDatabase, type Project } from '@/api/projects';
import Badge from '@/components/ui/Badge.vue';
import Card from '@/components/ui/Card.vue';

const props = defineProps<{ project: Project }>();

// Provision status only — the worker owns transitions (AGENTS); password
// never crosses the API (M6.5).
const database = useQuery({
  queryKey: computed(() => ['database', props.project.id]),
  queryFn: () => getDatabase(props.project.id),
  retry: false, // 404 = not provisioned yet, not a transient failure
  enabled: computed(() => props.project.wantsDb),
});
</script>

<template>
  <Card class="p-4 text-sm">
    <template v-if="!project.wantsDb">
      <h2 class="mb-1 text-sm font-semibold">Database</h2>
      <p class="text-muted-foreground">
        This project has no database. Create a new project with “Needs a Postgres database” to get
        one (projects are fixed at creation time).
      </p>
    </template>

    <template v-else-if="database.isPending.value">
      <p class="text-muted-foreground">Loading…</p>
    </template>

    <template v-else-if="database.error.value instanceof ApiError && database.error.value.status === 404">
      <h2 class="mb-1 text-sm font-semibold">Database</h2>
      <p class="text-muted-foreground">
        Not provisioned yet — the first deploy creates the role and database automatically (plan §8.5).
      </p>
    </template>

    <template v-else-if="database.isError.value">
      <p class="text-destructive">Could not load database status.</p>
    </template>

    <template v-else-if="database.data.value">
      <div class="mb-3 flex items-center gap-2">
        <h2 class="text-sm font-semibold">Database</h2>
        <Badge
          :class="
            database.data.value.status === 'ready'
              ? 'border-transparent bg-emerald-600 text-white'
              : 'border-transparent bg-amber-500 text-white'
          "
        >
          {{ database.data.value.status }}
        </Badge>
      </div>
      <dl class="grid grid-cols-[auto_1fr] gap-x-6 gap-y-2 text-muted-foreground">
        <dt class="font-medium text-foreground">Name</dt>
        <dd class="font-mono">{{ database.data.value.dbName }}</dd>
        <dt class="font-medium text-foreground">User</dt>
        <dd class="font-mono">{{ database.data.value.dbUser }}</dd>
        <dt class="font-medium text-foreground">Created</dt>
        <dd>{{ new Date(database.data.value.createdAt).toLocaleString() }}</dd>
      </dl>
      <p class="mt-3 text-xs text-muted-foreground">
        Connect from your app with <code class="font-mono">DATABASE_URL</code> (injected at container
        start; restart after env changes). Host <span class="font-mono">student-db:5432</span>.
      </p>
    </template>
  </Card>
</template>
