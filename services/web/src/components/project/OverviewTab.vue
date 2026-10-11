<script setup lang="ts">
import { computed, ref } from 'vue';
import { useMutation, useQueryClient } from '@tanstack/vue-query';
import { ApiError } from '@/api/client';
import { isTransientStatus, projectAction, uploadDeployment, type ActionType, type Project } from '@/api/projects';
import Button from '@/components/ui/Button.vue';
import Card from '@/components/ui/Card.vue';
import AlertDialog from '@/components/ui/AlertDialog.vue';
import AlertDialogAction from '@/components/ui/AlertDialogAction.vue';
import AlertDialogCancel from '@/components/ui/AlertDialogCancel.vue';
import AlertDialogContent from '@/components/ui/AlertDialogContent.vue';
import AlertDialogDescription from '@/components/ui/AlertDialogDescription.vue';
import AlertDialogFooter from '@/components/ui/AlertDialogFooter.vue';
import AlertDialogHeader from '@/components/ui/AlertDialogHeader.vue';
import AlertDialogTitle from '@/components/ui/AlertDialogTitle.vue';

const props = defineProps<{ project: Project }>();
const queryClient = useQueryClient();
const actionError = ref('');

function refresh() {
  void queryClient.invalidateQueries({ queryKey: ['project', props.project.id] });
  void queryClient.invalidateQueries({ queryKey: ['projects'] });
  void queryClient.invalidateQueries({ queryKey: ['deployments', props.project.id] });
}

const runAction = useMutation({
  mutationFn: (type: ActionType) => projectAction(props.project.id, type),
  onSuccess: refresh,
  onError: (e) => {
    actionError.value = e instanceof ApiError ? e.message : 'action failed';
  },
});

const upload = useMutation({
  mutationFn: (file: File) => uploadDeployment(props.project.id, file),
  onSuccess: refresh,
  onError: (e) => {
    actionError.value = e instanceof ApiError ? e.message : 'upload failed';
  },
});

// Confirmations on Stop/Redeploy only (M6.3) — shadcn AlertDialog, not
// window.confirm: focus trap + Esc + accessible description for free.
const confirmType = ref<ActionType | null>(null);

const confirmCopy: Record<string, { title: string; description: string; confirm: string }> = {
  stop: {
    title: 'Stop this project?',
    description: 'The app URL will stop responding until you start it again.',
    confirm: 'Stop',
  },
  redeploy: {
    title: 'Redeploy?',
    description: 'A new deployment starts now from the latest stored source.',
    confirm: 'Redeploy',
  },
};

function requestAction(type: ActionType) {
  actionError.value = '';
  if (type === 'stop' || type === 'redeploy') {
    confirmType.value = type;
    return;
  }
  runAction.mutate(type);
}

function confirmAction() {
  const type = confirmType.value;
  confirmType.value = null;
  if (type) runAction.mutate(type);
}

function onFilePick(event: Event) {
  const input = event.target as HTMLInputElement;
  const file = input.files?.[0];
  input.value = ''; // allow re-picking the same file after a failure
  if (!file) return;
  actionError.value = '';
  upload.mutate(file);
}

const busy = computed(() => isTransientStatus(props.project.status));
</script>

<template>
  <div>
    <Card class="p-4">
      <div class="flex flex-wrap items-center gap-2">
        <Button
          variant="destructive"
          :disabled="busy || project.status !== 'running' || runAction.isPending.value"
          @click="requestAction('stop')"
        >
          Stop
        </Button>
        <Button
          :disabled="busy || !['stopped', 'failed'].includes(project.status) || runAction.isPending.value"
          @click="requestAction('start')"
        >
          Start
        </Button>
        <Button
          variant="secondary"
          :disabled="busy || !project.currentDeploymentId || runAction.isPending.value"
          @click="requestAction('restart')"
        >
          Restart
        </Button>
        <Button
          variant="outline"
          :disabled="busy || !project.currentDeploymentId || runAction.isPending.value"
          @click="requestAction('redeploy')"
        >
          Redeploy
        </Button>

        <span class="w-4" />
        <label
          class="inline-flex h-9 cursor-pointer items-center rounded-md border border-input bg-background px-4 text-sm font-medium shadow-sm hover:bg-accent"
        >
          {{ upload.isPending.value ? 'Uploading…' : 'Upload zip' }}
          <input
            type="file"
            accept=".zip"
            class="hidden"
            :disabled="busy || upload.isPending.value"
            @change="onFilePick"
          />
        </label>
      </div>

      <p v-if="actionError" class="mt-3 text-sm text-destructive" role="alert">{{ actionError }}</p>
      <p v-else-if="busy" class="mt-3 text-sm text-muted-foreground">
        {{ project.status === 'deploying' ? 'Deploying — status refreshes automatically…' : 'Working…' }}
      </p>
    </Card>

    <dl class="mt-5 grid grid-cols-[auto_1fr] gap-x-6 gap-y-2 text-sm text-muted-foreground">
      <dt class="font-medium text-foreground">Slug</dt>
      <dd>{{ project.slug }}</dd>
      <dt class="font-medium text-foreground">Created</dt>
      <dd>{{ new Date(project.createdAt).toLocaleString() }}</dd>
      <dt class="font-medium text-foreground">Desired state</dt>
      <dd>{{ project.desiredState }}</dd>
    </dl>

    <AlertDialog :open="confirmType !== null" @update:open="(v) => !v && (confirmType = null)">
      <AlertDialogContent v-if="confirmType">
        <AlertDialogHeader>
          <AlertDialogTitle>{{ confirmCopy[confirmType].title }}</AlertDialogTitle>
          <AlertDialogDescription>{{ confirmCopy[confirmType].description }}</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction @click="confirmAction">{{ confirmCopy[confirmType].confirm }}</AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  </div>
</template>
