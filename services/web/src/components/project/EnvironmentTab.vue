<script setup lang="ts">
import { computed, ref } from 'vue';
import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query';
import { useField, useForm } from 'vee-validate';
import { toTypedSchema } from '@vee-validate/zod';
import * as z from 'zod';
import { ApiError } from '@/api/client';
import { deleteEnv, listEnv, projectAction, putEnv, putEnvBulk, type Project } from '@/api/projects';
import { parseEnvFile, toEnvFile } from '@/lib/envFile';
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

// Spec §M6.5: the banner carries a Restart button — same action as Overview,
// invalidates the shared project key so the header badge updates everywhere.
const restart = useMutation({
  mutationFn: () => projectAction(props.project.id, 'restart'),
  onSuccess: () => {
    changed.value = false;
    void queryClient.invalidateQueries({ queryKey: ['project', props.project.id] });
    void queryClient.invalidateQueries({ queryKey: ['projects'] });
  },
  onError: (e) => {
    formError.value = e instanceof ApiError ? e.message : 'restart failed';
  },
});

const submit = handleSubmit((v) => {
  formError.value = '';
  save.mutate({ key: v.key, value: v.value });
});

// --- bulk .env editor (plan §M6.5) -----------------------------------------
// Paste a whole .env; parsed live, diffed against the server state, then
// applied through the same per-key PUT/DELETE endpoints (confirm shows the
// diff). Keys with line breaks can't round-trip line-based parsing — they
// are excluded from seed AND diff so a paste can never delete them.

const bulkOpen = ref(false);
const bulkText = ref('');
const bulkError = ref('');
const bulkProtected = ref<string[]>([]); // multi-line-valued keys, never deleted
const bulkConfirm = ref(false);

function toggleBulk() {
  bulkOpen.value = !bulkOpen.value;
  if (bulkOpen.value) {
    const { text, multiline } = toEnvFile(env.data.value?.items ?? []);
    bulkText.value = text;
    bulkProtected.value = multiline;
    bulkError.value = '';
  }
}

const parsed = computed(() => parseEnvFile(bulkText.value));

const bulkDiff = computed(() => {
  const current = new Map((env.data.value?.items ?? []).map((e) => [e.key, e.value]));
  const seen = new Map(parsed.value.vars.map((v) => [v.key, v.value]));
  const puts = parsed.value.vars.filter((v) => !current.has(v.key) || current.get(v.key) !== v.value);
  const dels = [...current.keys()].filter((k) => !seen.has(k) && !bulkProtected.value.includes(k));
  const unchanged = parsed.value.vars.length - puts.length;
  return { puts, dels, unchanged };
});

const bulkApply = useMutation({
  // One request: the endpoint validates everything, checks quota against the
  // final state and applies set+remove in a single transaction — no
  // partial-apply path to recover from (plan §M6.5).
  mutationFn: (plan: typeof bulkDiff.value) =>
    putEnvBulk(props.project.id, {
      set: Object.fromEntries(plan.puts.map((p) => [p.key, p.value])),
      remove: plan.dels,
    }),
  onSuccess: () => {
    changed.value = true;
    bulkConfirm.value = false;
    invalidate();
  },
  onError: (e) => {
    bulkConfirm.value = false;
    bulkError.value = e instanceof ApiError ? e.message : 'bulk apply failed';
  },
});

function applyBulk() {
  bulkError.value = '';
  bulkApply.mutate(bulkDiff.value);
}
</script>

<template>
  <div class="flex flex-col gap-4">
    <div
      v-if="changed"
      class="flex items-center gap-2 rounded-lg border border-amber-300 bg-amber-50 px-4 py-2 text-sm text-amber-900"
      role="status"
    >
      <Badge class="border-transparent bg-amber-500 text-white">restart required</Badge>
      Environment changed — restart the project to apply it.
      <span class="flex-1" />
      <Button
        size="sm"
        variant="outline"
        :disabled="restart.isPending.value"
        @click="restart.mutate()"
      >
        {{ restart.isPending.value ? 'Restarting…' : 'Restart now' }}
      </Button>
      <button class="underline" @click="changed = false">dismiss</button>
    </div>

    <Card class="p-4">
      <div class="mb-3 flex items-center gap-2">
        <h2 class="text-sm font-semibold">Variables</h2>
        <span class="text-xs text-muted-foreground">{{ env.data.value?.total ?? 0 }} / 50</span>
        <span class="flex-1" />
        <Button size="sm" variant="ghost" @click="toggleBulk">
          {{ bulkOpen ? 'Hide bulk editor' : 'Bulk edit (.env)' }}
        </Button>
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
            :value="key.value.value"
            @input="key.handleChange"
            @blur="key.handleBlur"
          />
          <p v-if="errors.key" class="text-sm text-destructive">{{ errors.key }}</p>
        </div>
        <div class="flex flex-col gap-2">
          <Label for="env-value">Value</Label>
          <Input
            id="env-value"
            placeholder="hello world"
            :value="value.value.value"
            @input="value.handleChange"
            @blur="value.handleBlur"
          />
          <p v-if="errors.value" class="text-sm text-destructive">{{ errors.value }}</p>
        </div>
        <p v-if="formError" class="text-sm text-destructive" role="alert">{{ formError }}</p>
        <div>
          <Button type="submit" :disabled="save.isPending.value">
            {{ save.isPending.value ? 'Saving…' : editing ? `Save ${editing}` : 'Add variable' }}
          </Button>
        </div>
      </form>

      <div v-if="bulkOpen" class="mt-4 border-t pt-4">
        <p class="mb-2 text-xs text-muted-foreground">
          Regular <code class="font-mono">.env</code> syntax: <code class="font-mono">KEY=value</code>
          lines, <code class="font-mono">#</code> comments on their own line, optional
          <code class="font-mono">export</code> and quotes. Applying updates changed keys, adds new
          ones and deletes keys missing from the text.
        </p>
        <textarea
          v-model="bulkText"
          rows="8"
          spellcheck="false"
          class="w-full rounded-md border bg-foreground px-3 py-2 font-mono text-sm text-background outline-none focus:ring-2 focus:ring-ring"
          placeholder="GREETING=hello&#10;FEATURE_FLAG=on"
          aria-label="Bulk environment editor (.env syntax)"
        />
        <div v-if="bulkProtected.length" class="mt-1 text-xs text-amber-700">
          {{ bulkProtected.join(', ') }} contain line breaks — not shown here and never deleted by a
          bulk apply.
        </div>
        <ul v-if="parsed.errors.length" class="mt-1 flex flex-col text-xs text-destructive" role="alert">
          <li v-for="e in parsed.errors" :key="e.line">line {{ e.line }}: {{ e.message }}</li>
        </ul>
        <p v-else class="mt-1 text-xs text-muted-foreground">
          {{ parsed.vars.length }} variables —
          {{ bulkDiff.puts.filter((p) => !(env.data.value?.items ?? []).some((e) => e.key === p.key)).length }} new,
          {{ bulkDiff.puts.length }} to save,
          {{ bulkDiff.dels.length }} to delete,
          {{ bulkDiff.unchanged }} unchanged
        </p>
        <p v-if="bulkError" class="mt-1 text-sm text-destructive" role="alert">{{ bulkError }}</p>
        <div class="mt-2">
          <Button
            size="sm"
            :disabled="parsed.errors.length > 0 || bulkApply.isPending.value || (bulkDiff.puts.length === 0 && bulkDiff.dels.length === 0)"
            @click="bulkConfirm = true"
          >
            {{ bulkApply.isPending.value ? 'Applying…' : 'Review & apply' }}
          </Button>
        </div>
      </div>
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

    <AlertDialog :open="bulkConfirm" @update:open="(v) => !v && (bulkConfirm = false)">
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Apply bulk changes?</AlertDialogTitle>
          <AlertDialogDescription>
            {{ bulkDiff.puts.length }} variable(s) will be saved,
            {{ bulkDiff.dels.length }} deleted
            <template v-if="bulkDiff.dels.length">({{ bulkDiff.dels.join(', ') }})</template>.
            A restart is required to apply them to the running container.
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction @click="applyBulk">Apply</AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  </div>
</template>
