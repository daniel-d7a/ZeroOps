<script setup lang="ts">
import { computed } from 'vue';
import { useRoute, useRouter, RouterLink } from 'vue-router';
import UsersPanel from '@/components/admin/UsersPanel.vue';
import ProjectsPanel from '@/components/admin/ProjectsPanel.vue';
import SystemPanel from '@/components/admin/SystemPanel.vue';
import Tabs from '@/components/ui/Tabs.vue';
import TabsContent from '@/components/ui/TabsContent.vue';
import TabsList from '@/components/ui/TabsList.vue';
import TabsTrigger from '@/components/ui/TabsTrigger.vue';

// Tab selection lives in ?tab= so links are shareable; switching tab resets
// the shared ?offset= (the other list starts at page 1).
const TABS = ['users', 'projects', 'system'] as const;
const route = useRoute();
const router = useRouter();

const tab = computed<string>({
  get: () => (TABS as readonly string[]).includes(String(route.query.tab)) ? String(route.query.tab) : 'users',
  set: (value) => {
    void router.replace({ query: value === 'users' ? {} : { tab: value } });
  },
});
</script>

<template>
  <main class="mx-auto mt-10 w-full max-w-4xl px-4">
    <header class="mb-6 flex items-baseline gap-3">
      <h1 class="text-xl font-semibold">Admin</h1>
      <span class="flex-1" />
      <RouterLink class="text-sm text-muted-foreground underline" to="/">Back to projects</RouterLink>
    </header>

    <Tabs v-model="tab">
      <TabsList class="mb-4">
        <TabsTrigger value="users">Users</TabsTrigger>
        <TabsTrigger value="projects">Projects</TabsTrigger>
        <TabsTrigger value="system">System</TabsTrigger>
      </TabsList>
      <TabsContent value="users"><UsersPanel /></TabsContent>
      <TabsContent value="projects"><ProjectsPanel /></TabsContent>
      <TabsContent value="system"><SystemPanel /></TabsContent>
    </Tabs>
  </main>
</template>
