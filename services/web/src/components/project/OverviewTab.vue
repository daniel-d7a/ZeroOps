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
  mutationFn: (file: File) =>
    uploadDeployment(props.project.id, file, (pct) => {
      uploadPct.value = pct;
    }),
  onSuccess: refresh,
  onSettled: () => {
    uploadPct.value = null;
  },
  onError: (e) => {
    actionError.value = e instanceof ApiError ? e.message : 'upload failed';
  },
});

// Drag-and-drop upload with progress (plan §9) — XHR because fetch has no
// upload-progress events.
const uploadPct = ref<number | null>(null);
const dragOver = ref(false);

// Confirmations on Stop/Redeploy only (M6.3) — shadcn AlertDialog, not
// window.confirm: focus trap + Esc + accessible description for free.
// dialogOpen is separate from confirmType: DialogClose's own onClick (close)
// fires BEFORE our @click, so clearing the type on update:open would wipe it
// before confirmAction reads it — the confirm would silently no-op.
const confirmType = ref<ActionType | null>(null);
const dialogOpen = ref(false);

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
    dialogOpen.value = true;
    return;
  }
  runAction.mutate(type);
}

function confirmAction() {
  // Read the type first: DialogClose's close handler may already have flipped
  // dialogOpen, but confirmType is only cleared here.
  const type = confirmType.value;
  confirmType.value = null;
  dialogOpen.value = false;
  if (type) runAction.mutate(type);
}

function startUpload(file: File | undefined) {
  if (!file) return;
  actionError.value = '';
  if (!file.name.toLowerCase().endsWith('.zip')) {
    actionError.value = 'only .zip archives can be deployed';
    return;
  }
  upload.mutate(file);
}

function onFilePick(event: Event) {
  const input = event.target as HTMLInputElement;
  const file = input.files?.[0];
  input.value = ''; // allow re-picking the same file after a failure
  startUpload(file);
}

function onDrop(event: DragEvent) {
  dragOver.value = false;
  startUpload(event.dataTransfer?.files?.[0]);
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
      </div>

      <div
        class="mt-3 flex flex-col items-center justify-center gap-1 rounded-lg border-2 border-dashed px-4 py-6 text-sm transition-colors"
        :class="dragOver ? 'border-foreground bg-accent' : 'border-border'"
        @dragover.prevent="dragOver = true"
        @dragleave.prevent="dragOver = false"
        @drop.prevent="onDrop"
      >
        <template v-if="upload.isPending.value">
          <span class="text-muted-foreground">Uploading… {{ uploadPct ?? 0 }}%</span>
          <div class="mt-1 h-1.5 w-48 overflow-hidden rounded-full bg-secondary">
            <div class="h-full bg-primary transition-all" :style="{ width: `${uploadPct ?? 0}%` }" />
          </div>
        </template>
        <template v-else>
          <span class="text-muted-foreground">Drag a zip here</span>
          <label class="cursor-pointer underline">
            or browse…
            <input type="file" accept=".zip" class="hidden" :disabled="busy" @change="onFilePick" />
          </label>
        </template>
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

    <AlertDialog :open="dialogOpen" @update:open="(v) => (dialogOpen = v)">
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
