<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue';
import { useQuery } from '@tanstack/vue-query';
import { api, ApiError } from '@/api/client';
import Badge from '@/components/ui/Badge.vue';
import Card from '@/components/ui/Card.vue';

interface Deployment {
  id: string;
  kind: 'upload' | 'redeploy';
  status: 'queued' | 'building' | 'built' | 'starting' | 'live' | 'failed' | 'superseded';
  error: string | null;
  attempt: number;
  createdAt: string;
  startedAt: string | null;
  finishedAt: string | null;
  originalFilename: string | null; // source zip name (plan §9 history)
}

interface LogLine {
  id: number;
  stream: string; // 'build' | 'system'
  line: string;
  createdAt: string;
}

const props = defineProps<{ projectId: string }>();

// Deployment statuses the worker is still writing (plan §8) — worth polling;
// live/failed/superseded are terminal.
const ACTIVE = new Set<Deployment['status']>(['queued', 'building', 'built', 'starting']);

const deployments = useQuery({
  queryKey: computed(() => ['deployments', props.projectId]),
  queryFn: () => api<{ items: Deployment[]; total: number }>(`/projects/${props.projectId}/deployments`),
  refetchInterval: (query) =>
    query.state.data?.items.some((d) => ACTIVE.has(d.status)) ? 2000 : false,
});

// --- log viewer: cursor accumulation (plan §7 poll ?after=<last id>) ------
const selectedId = ref<string | null>(null);
const lines = ref<LogLine[]>([]);
const nextAfter = ref(0);
const pulling = ref(false);
let lastPulledStatus = ''; // pull once more when a deployment turns terminal

const selected = computed(
  () => deployments.data.value?.items.find((d) => d.id === selectedId.value) ?? null,
);

async function pullLogs() {
  if (pulling.value || !selectedId.value) return;
  pulling.value = true;
  try {
    const res = await api<{ items: LogLine[]; nextAfter: number }>(
      `/deployments/${selectedId.value}/logs?after=${nextAfter.value}`,
    );
    if (res.items.length) lines.value.push(...res.items);
    nextAfter.value = res.nextAfter;
  } catch {
    // transient fetch failure — next poll retries via the watch
  } finally {
    pulling.value = false;
  }
}

function select(id: string) {
  if (selectedId.value === id) return;
  selectedId.value = id;
  lines.value = [];
  nextAfter.value = 0;
  lastPulledStatus = '';
  void pullLogs();
}

// Newest first in the list; auto-select it (M6.4: logs are the point).
watch(deployments.data, (data) => {
  if (!data?.items.length) return;
  if (!selectedId.value) {
    select(data.items[0]!.id);
    return;
  }
  const d = selected.value;
  if (!d) return;
  // active -> keep pulling; just turned terminal -> one final pull
  if (ACTIVE.has(d.status) || d.status !== lastPulledStatus) {
    lastPulledStatus = d.status;
    void pullLogs();
  }
});

const logBox = ref<HTMLElement | null>(null);
watch(lines, async () => {
  await nextTick();
  if (logBox.value) logBox.value.scrollTop = logBox.value.scrollHeight;
});

const statusClass: Record<string, string> = {
  live: 'bg-emerald-600 text-white border-transparent',
  failed: 'bg-destructive text-destructive-foreground border-transparent',
  building: 'bg-amber-500 text-white border-transparent',
  starting: 'bg-amber-500 text-white border-transparent',
  queued: 'bg-amber-500 text-white border-transparent',
  superseded: 'bg-secondary text-secondary-foreground border-transparent',
};
</script>

<template>
  <div class="grid gap-4 md:grid-cols-[18rem_1fr]">
    <div>
      <p v-if="deployments.isPending.value" class="text-sm text-muted-foreground">Loading…</p>
      <p v-else-if="deployments.isError.value" class="text-sm text-destructive">
        {{ deployments.error.value instanceof ApiError ? deployments.error.value.message : 'failed to load' }}
      </p>
      <p v-else-if="deployments.data.value?.items.length === 0" class="text-sm text-muted-foreground">
        No deployments yet — upload a zip on the Overview tab.
      </p>
      <ul v-else class="flex flex-col gap-2">
        <li v-for="d in deployments.data.value?.items" :key="d.id">
          <button
            class="w-full rounded-lg border p-3 text-left text-sm transition-colors hover:bg-accent"
            :class="d.id === selectedId ? 'border-foreground bg-accent' : 'border-border'"
            @click="select(d.id)"
          >
            <span class="flex items-center gap-2">
              <Badge :class="statusClass[d.status] ?? ''">{{ d.status }}</Badge>
              <span class="text-xs text-muted-foreground">{{ d.kind }}</span>
              <span v-if="d.originalFilename" class="truncate text-xs text-muted-foreground">
                {{ d.originalFilename }}
              </span>
              <span class="flex-1" />
              <span class="text-xs text-muted-foreground">
                {{ new Date(d.createdAt).toLocaleTimeString() }}
              </span>
            </span>
            <span v-if="d.error" class="mt-1 block truncate text-xs text-destructive">{{ d.error }}</span>
          </button>
        </li>
      </ul>
    </div>

    <Card v-if="selectedId" class="overflow-hidden">
      <div class="flex items-center gap-2 border-b px-4 py-2 text-xs text-muted-foreground">
        <span>logs</span>
        <span v-if="selected && ACTIVE.has(selected.status)" class="animate-pulse">● live</span>
      </div>
      <div ref="logBox" class="h-96 overflow-y-auto bg-zinc-950 p-3 font-mono text-xs leading-5 text-zinc-100">
        <p v-if="lines.length === 0" class="text-zinc-500">No log output yet…</p>
        <p
          v-for="l in lines"
          :key="l.id"
          :class="l.stream === 'system' ? 'text-amber-400' : 'text-zinc-300'"
        >
          {{ l.line }}
        </p>
      </div>
    </Card>
  </div>
</template>
