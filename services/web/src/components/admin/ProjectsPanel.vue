<script setup lang="ts">
import { computed, ref } from 'vue';
import { useMutation, useQuery } from '@tanstack/vue-query';
import { ApiError } from '@/api/client';
import { adminStop, listAdminProjects, type AdminProject } from '@/api/admin';
import { useOffsetParam } from '@/composables/useOffsetParam';
import AlertDialog from '@/components/ui/AlertDialog.vue';
import AlertDialogAction from '@/components/ui/AlertDialogAction.vue';
import AlertDialogCancel from '@/components/ui/AlertDialogCancel.vue';
import AlertDialogContent from '@/components/ui/AlertDialogContent.vue';
import AlertDialogDescription from '@/components/ui/AlertDialogDescription.vue';
import AlertDialogFooter from '@/components/ui/AlertDialogFooter.vue';
import AlertDialogHeader from '@/components/ui/AlertDialogHeader.vue';
import AlertDialogTitle from '@/components/ui/AlertDialogTitle.vue';
import Badge from '@/components/ui/Badge.vue';
import Button from '@/components/ui/Button.vue';
import Card from '@/components/ui/Card.vue';
import Pager from '@/components/admin/Pager.vue';

// Offset lives in ?offset= (useOffsetParam) — shareable and reload-proof.
const { offset, setOffset } = useOffsetParam();
const actionError = ref('');
const target = ref<AdminProject | null>(null);

const projects = useQuery({
  queryKey: computed(() => ['admin-projects', offset.value]),
  queryFn: () => listAdminProjects(offset.value),
  refetchInterval: (q) => (q.state.data?.items.some((p) => p.status === 'deploying') ? 3000 : false),
});

const statusClass: Record<string, string> = {
  running: 'bg-emerald-600 text-white border-transparent',
  deploying: 'bg-amber-500 text-white border-transparent',
  failed: 'bg-destructive text-destructive-foreground border-transparent',
  stopped: 'bg-secondary text-secondary-foreground border-transparent',
};

const stop = useMutation({
  mutationFn: (id: string) => adminStop(id),
  onSuccess: () => {
    target.value = null;
    void projects.refetch();
  },
  onError: (e) => {
    target.value = null;
    actionError.value = e instanceof ApiError ? e.message : 'stop failed';
  },
});
</script>

<template>
  <div class="flex flex-col gap-3">
    <p v-if="actionError" class="text-sm text-destructive" role="alert">{{ actionError }}</p>
    <p v-if="projects.isPending.value" class="text-sm text-muted-foreground">Loading…</p>
    <p v-else-if="projects.isError.value" class="text-sm text-destructive">Failed to load projects.</p>
    <template v-else>
      <ul class="flex flex-col gap-2">
        <li v-for="p in projects.data.value?.items" :key="p.id">
          <Card class="flex items-center gap-3 p-3 text-sm">
            <span class="font-medium">{{ p.name }}</span>
            <span class="text-xs text-muted-foreground">({{ p.ownerEmail }})</span>
            <Badge :class="statusClass[p.status] ?? ''">{{ p.status }}</Badge>
            <span v-if="p.wantsDb" class="text-xs text-muted-foreground">db</span>
            <span v-if="p.node" class="font-mono text-xs text-muted-foreground" title="worker node">{{ p.node }}</span>
            <span class="flex-1" />
            <a :href="p.url" class="text-muted-foreground underline" target="_blank" rel="noreferrer">{{ p.url }}</a>
            <Button
              size="sm"
              variant="outline"
              :disabled="p.status !== 'running'"
              title="Stop runs a project action (plan §9)"
              @click="actionError = ''; target = p"
            >
              Stop
            </Button>
          </Card>
        </li>
      </ul>
      <Pager
        :total="projects.data.value?.total ?? 0"
        :offset="offset"
        :limit="20"
        @update:offset="setOffset"
      />
    </template>

    <AlertDialog :open="target !== null" @update:open="(v) => !v && (target = null)">
      <AlertDialogContent v-if="target">
        <AlertDialogHeader>
          <AlertDialogTitle>Stop {{ target.name }}?</AlertDialogTitle>
          <AlertDialogDescription>
            {{ target.ownerEmail }}'s container stops serving; the project can be started again
            from its detail page.
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction @click="stop.mutate(target.id)">Stop</AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  </div>
</template>
