<script setup lang="ts">
import { computed, ref } from 'vue';
import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query';
import { useField, useForm } from 'vee-validate';
import { toTypedSchema } from '@vee-validate/zod';
import * as z from 'zod';
import { ApiError } from '@/api/client';
import { deleteEnv, listEnv, putEnv, type Project } from '@/api/projects';
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
import Input from '@/components/ui/Input.vue';
import Label from '@/components/ui/Label.vue';

const props = defineProps<{ project: Project }>();
const queryClient = useQueryClient();
const formError = ref('');
const changed = ref(false); // drives the "restart required" banner
const deleteKey = ref<string | null>(null);

const env = useQuery({
  queryKey: computed(() => ['env', props.project.id]),
  queryFn: () => listEnv(props.project.id),
});

// Mirrors API rules (plan §7/M5.2): KEY ^[A-Z_][A-Z0-9_]* ≤128, value ≤4KB,
// reserved PORT/DATABASE_URL belong to the platform.
const RESERVED = new Set(['PORT', 'DATABASE_URL']);
const schema = toTypedSchema(
  z.object({
    key: z
      .string()
      .min(1, 'Key is required')
      .max(128, 'At most 128 characters')
      .regex(/^[A-Z_][A-Z0-9_]*$/, 'Uppercase letters, digits and underscore; must not start with a digit')
      .refine((k) => !RESERVED.has(k), 'PORT and DATABASE_URL are reserved by the platform'),
    value: z.string().min(1, 'Value is required').max(4096, 'At most 4096 characters'),
  }),
);
const { handleSubmit, errors, resetForm } = useForm({
  validationSchema: schema,
  initialValues: { key: '', value: '' },
});
const key = useField<string>('key');
const value = useField<string>('value');

const editing = ref<string | null>(null); // null = adding a new key

function startEdit(k: string, v: string) {
  editing.value = k;
  resetForm({ values: { key: k, value: v } });
  formError.value = '';
}

function startAdd() {
  editing.value = null;
  resetForm();
  formError.value = '';
}

function invalidate() {
  void queryClient.invalidateQueries({ queryKey: ['env', props.project.id] });
}

const save = useMutation({
  mutationFn: (input: { key: string; value: string }) => putEnv(props.project.id, input.key, input.value),
  onSuccess: () => {
    changed.value = true;
    startAdd();
    invalidate();
  },
  onError: (e) => {
    formError.value = e instanceof ApiError ? e.message : 'could not save variable';
  },
});

const remove = useMutation({
  mutationFn: (k: string) => deleteEnv(props.project.id, k),
  onSuccess: () => {
    changed.value = true;
    if (editing.value && deleteKey.value === editing.value) startAdd();
    deleteKey.value = null;
    invalidate();
  },
  onError: (e) => {
    deleteKey.value = null;
    formError.value = e instanceof ApiError ? e.message : 'could not delete variable';
  },
});

const submit = handleSubmit((v) => {
  formError.value = '';
  save.mutate({ key: v.key, value: v.value });
});
</script>

<template>
  <div class="flex flex-col gap-4">
    <div
      v-if="changed"
      class="flex items-center gap-2 rounded-lg border border-amber-300 bg-amber-50 px-4 py-2 text-sm text-amber-900"
      role="status"
    >
      <Badge class="border-transparent bg-amber-500 text-white">restart required</Badge>
      Environment changed — restart the project from the Overview tab to apply it.
      <button class="ml-auto underline" @click="changed = false">dismiss</button>
    </div>

    <Card class="p-4">
      <div class="mb-3 flex items-center gap-2">
        <h2 class="text-sm font-semibold">Variables</h2>
        <span class="text-xs text-muted-foreground">{{ env.data.value?.total ?? 0 }} / 50</span>
        <span class="flex-1" />
        <Button v-if="editing" size="sm" variant="outline" @click="startAdd">Add new</Button>
      </div>

      <p v-if="env.isPending.value" class="text-sm text-muted-foreground">Loading…</p>
      <p v-else-if="env.data.value?.items.length === 0" class="text-sm text-muted-foreground">
        No environment variables yet.
      </p>
      <ul v-else class="mb-4 flex flex-col gap-2">
        <li
          v-for="row in env.data.value?.items"
          :key="row.key"
          class="flex items-center gap-3 rounded-md border px-3 py-2 text-sm"
          :class="editing === row.key ? 'border-foreground bg-accent' : ''"
        >
          <code class="font-mono font-semibold">{{ row.key }}</code>
          <span class="truncate text-muted-foreground">{{ row.value }}</span>
          <span class="flex-1" />
          <button class="text-xs underline" @click="startEdit(row.key, row.value)">Edit</button>
          <button class="text-xs text-destructive underline" @click="deleteKey = row.key">Delete</button>
        </li>
      </ul>

      <form class="flex flex-col gap-3" novalidate @submit="submit">
        <div class="flex flex-col gap-2">
          <Label for="env-key">Key</Label>
          <Input
            id="env-key"
            placeholder="GREETING"
            class="font-mono"
            :disabled="editing !== null"
            v-bind="key"
          />
          <p v-if="errors.key" class="text-sm text-destructive">{{ errors.key }}</p>
        </div>
        <div class="flex flex-col gap-2">
          <Label for="env-value">Value</Label>
          <Input id="env-value" placeholder="hello world" v-bind="value" />
          <p v-if="errors.value" class="text-sm text-destructive">{{ errors.value }}</p>
        </div>
        <p v-if="formError" class="text-sm text-destructive" role="alert">{{ formError }}</p>
        <div>
          <Button type="submit" :disabled="save.isPending.value">
            {{ save.isPending.value ? 'Saving…' : editing ? `Save ${editing}` : 'Add variable' }}
          </Button>
        </div>
      </form>
    </Card>

    <AlertDialog :open="deleteKey !== null" @update:open="(v) => !v && (deleteKey = null)">
      <AlertDialogContent v-if="deleteKey">
        <AlertDialogHeader>
          <AlertDialogTitle>Delete {{ deleteKey }}?</AlertDialogTitle>
          <AlertDialogDescription>
            The variable is removed immediately. The running container keeps its old value until restart.
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction @click="remove.mutate(deleteKey)">Delete</AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  </div>
</template>
