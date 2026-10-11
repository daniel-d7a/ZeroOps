<script setup lang="ts">
import { computed, ref } from 'vue';
import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query';
import { api, ApiError } from '@/api/client';
import { listSources, type Project, type Source } from '@/api/projects';
import AlertDialog from '@/components/ui/AlertDialog.vue';
import AlertDialogAction from '@/components/ui/AlertDialogAction.vue';
import AlertDialogCancel from '@/components/ui/AlertDialogCancel.vue';
import AlertDialogContent from '@/components/ui/AlertDialogContent.vue';
import AlertDialogDescription from '@/components/ui/AlertDialogDescription.vue';
import AlertDialogFooter from '@/components/ui/AlertDialogFooter.vue';
import AlertDialogHeader from '@/components/ui/AlertDialogHeader.vue';
import AlertDialogTitle from '@/components/ui/AlertDialogTitle.vue';
import Button from '@/components/ui/Button.vue';
import Card from '@/components/ui/Card.vue';

const props = defineProps<{ project: Project }>();
const queryClient = useQueryClient();
const actionError = ref('');
const confirmSource = ref<Source | null>(null);

const sources = useQuery({
  queryKey: computed(() => ['sources', props.project.id]),
  queryFn: () => listSources(props.project.id),
});

const redeploy = useMutation({
  mutationFn: (sourceId: string) => api(`/projects/${props.project.id}/redeploy`, { json: { sourceId } }),
  onSuccess: () => {
    confirmSource.value = null;
    void queryClient.invalidateQueries({ queryKey: ['project', props.project.id] });
    void queryClient.invalidateQueries({ queryKey: ['projects'] });
    void queryClient.invalidateQueries({ queryKey: ['deployments', props.project.id] });
  },
  onError: (e) => {
    confirmSource.value = null;
    actionError.value = e instanceof ApiError ? e.message : 'redeploy failed';
  },
});

function fmtSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}
</script>

<template>
  <div class="flex flex-col gap-4">
    <p class="text-sm text-muted-foreground">
      Saved upload zips — retention keeps the newest 5 plus the live one (plan §7). Redeploy runs a
      build from the stored file.
    </p>

    <p v-if="sources.isPending.value" class="text-sm text-muted-foreground">Loading…</p>
    <p v-else-if="sources.isError.value" class="text-sm text-destructive">
      {{ sources.error.value instanceof ApiError ? sources.error.value.message : 'failed to load sources' }}
    </p>
    <p v-else-if="sources.data.value?.items.length === 0" class="text-sm text-muted-foreground">
      No saved sources — upload a zip on the Overview tab.
    </p>
    <ul v-else class="flex flex-col gap-2">
      <li v-for="s in sources.data.value?.items" :key="s.id">
        <Card class="flex items-center gap-3 p-3 text-sm">
          <span class="font-medium">{{ s.originalFilename }}</span>
          <span class="text-muted-foreground">{{ fmtSize(s.sizeBytes) }}</span>
          <span class="text-xs text-muted-foreground">{{ new Date(s.createdAt).toLocaleString() }}</span>
          <span class="flex-1" />
          <Button
            size="sm"
            variant="outline"
            :disabled="redeploy.isPending.value"
            @click="actionError = ''; confirmSource = s"
          >
            Redeploy
          </Button>
        </Card>
      </li>
    </ul>

    <p v-if="actionError" class="text-sm text-destructive" role="alert">{{ actionError }}</p>

    <AlertDialog :open="confirmSource !== null" @update:open="(v) => !v && (confirmSource = null)">
      <AlertDialogContent v-if="confirmSource">
        <AlertDialogHeader>
          <AlertDialogTitle>Redeploy {{ confirmSource.originalFilename }}?</AlertDialogTitle>
          <AlertDialogDescription>
            A new deployment starts now from this stored zip — the current live version keeps serving
            until the new one passes its health check.
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction @click="redeploy.mutate(confirmSource.id)">Redeploy</AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  </div>
</template>
