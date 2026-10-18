<script setup lang="ts">
import { computed } from 'vue';
import { RouterLink, useRoute } from 'vue-router';
import { useQuery } from '@tanstack/vue-query';
import { ApiError } from '@/api/client';
import { getProject } from '@/api/projects';
import OverviewTab from '@/components/project/OverviewTab.vue';
import DeploymentsTab from '@/components/project/DeploymentsTab.vue';
import EnvironmentTab from '@/components/project/EnvironmentTab.vue';
import FilesTab from '@/components/project/FilesTab.vue';
import DatabaseTab from '@/components/project/DatabaseTab.vue';
import Badge from '@/components/ui/Badge.vue';
import Tabs from '@/components/ui/Tabs.vue';
import TabsContent from '@/components/ui/TabsContent.vue';
import TabsList from '@/components/ui/TabsList.vue';
import TabsTrigger from '@/components/ui/TabsTrigger.vue';

const route = useRoute();
const id = computed(() => route.params.id as string);

// Header query lives here (shared by every tab). Poll on a fixed interval:
// the worker moves status asynchronously (plan §8) and mid-action the pair
// stays coherent — the API never writes desired_state (schema default is
// 'running'), the worker writes status+desired_state atomically on success;
// they only diverge on defaults/failures — nothing to watch, so poll
// unconditionally. SSE in FIXLIST Phase 2 supersedes this.
const project = useQuery({
  queryKey: computed(() => ['project', id.value]),
  queryFn: () => getProject(id.value),
  refetchInterval: 2500,
});

const statusClass: Record<string, string> = {
  running: 'bg-emerald-600 text-white border-transparent',
  deploying: 'bg-amber-500 text-white border-transparent',
  failed: 'bg-destructive text-destructive-foreground border-transparent',
  stopped: 'bg-secondary text-secondary-foreground border-transparent',
};
</script>

<template>
  <main class="mx-auto mt-10 w-full max-w-4xl px-4">
    <p class="mb-4 text-sm text-muted-foreground">
      <RouterLink class="underline" to="/">Projects</RouterLink>
      <template v-if="project.data.value"> / {{ project.data.value.name }}</template>
    </p>

    <p v-if="project.isPending.value" class="text-sm text-muted-foreground">Loading…</p>
    <p v-else-if="project.isError.value" class="text-sm text-destructive">
      {{ project.error.value instanceof ApiError ? project.error.value.message : 'failed to load project' }}
    </p>

    <template v-else-if="project.data.value">
      <header class="mb-5 flex items-baseline gap-3">
        <h1 class="text-xl font-semibold">{{ project.data.value.name }}</h1>
        <Badge :class="statusClass[project.data.value.status] ?? ''">{{ project.data.value.status }}</Badge>
        <span v-if="project.data.value.wantsDb" class="text-xs text-muted-foreground">db</span>
        <span class="flex-1" />
        <a
          :href="project.data.value.url"
          class="text-sm text-muted-foreground underline"
          target="_blank"
          rel="noreferrer"
        >
          {{ project.data.value.url }}
        </a>
      </header>

      <Tabs default-value="overview">
        <TabsList class="mb-4">
          <TabsTrigger value="overview">Overview</TabsTrigger>
          <TabsTrigger value="deployments">Deployments</TabsTrigger>
          <TabsTrigger value="environment">Environment</TabsTrigger>
          <TabsTrigger value="files">Files</TabsTrigger>
          <TabsTrigger value="database">Database</TabsTrigger>
        </TabsList>
        <TabsContent value="overview">
          <OverviewTab :project="project.data.value" />
        </TabsContent>
        <TabsContent value="deployments">
          <DeploymentsTab :project-id="id" />
        </TabsContent>
        <TabsContent value="environment">
          <EnvironmentTab :project="project.data.value" />
        </TabsContent>
        <TabsContent value="files">
          <FilesTab :project="project.data.value" />
        </TabsContent>
        <TabsContent value="database">
          <DatabaseTab :project="project.data.value" />
        </TabsContent>
      </Tabs>
    </template>
  </main>
</template>
