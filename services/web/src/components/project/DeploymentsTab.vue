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
// Curation (FIXLIST 1.2): the viewer keeps a capped window — the store
// already prunes to the last 2000 lines per deployment, and the live view
// only ever needs the recent tail. MAX_LINES_PER_PULL is the API-side batch
// cap (deployments.service.ts): a full batch means "possibly more", which
// drives the terminal-pull loop below.
const MAX_LOG_LINES = 1000;
const MAX_LINES_PER_PULL = 500;

const selectedId = ref<string | null>(null);
const lines = ref<LogLine[]>([]);
const nextAfter = ref(0);
const pulling = ref(false);
let lastPulledStatus = ''; // pull once more when a deployment turns terminal
let lastBatchFull = false; // last pull filled a whole batch → maybe more?
let terminalTicks = 0; // dataUpdatedAt ticks since the terminal flip

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
    if (res.items.length) {
      lastBatchFull = res.items.length >= MAX_LINES_PER_PULL;
      lines.value.push(...res.items);
      // Drop from the front past the window: a chatty build must not grow
      // the DOM into tens of thousands of <p> nodes (FIXLIST 1.2).
      if (lines.value.length > MAX_LOG_LINES) {
        lines.value.splice(0, lines.value.length - MAX_LOG_LINES);
      }
    } else {
      lastBatchFull = false;
    }
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
  lastBatchFull = false;
  terminalTicks = 0;
  void pullLogs();
}

// Newest first in the list; auto-select it (M6.4: logs are the point).
// Watch dataUpdatedAt, not data: TanStack's structural sharing returns the
// same object reference when a 2s refetch is deep-equal — which is exactly
// what a long "building" phase looks like — so watching data freezes the log
// puller for the whole build. dataUpdatedAt moves on every successful fetch.
watch(
  () => deployments.dataUpdatedAt.value,
  () => {
    const data = deployments.data.value;
    if (!data?.items.length) return;
    if (!selectedId.value) {
      select(data.items[0]!.id);
      return;
    }
    const d = selected.value;
    if (!d) return;
    if (ACTIVE.has(d.status)) {
      lastPulledStatus = d.status;
      terminalTicks = 0;
      void pullLogs();
      return;
    }
    // Terminal: keep pulling until the tail is drained — a batched build
    // can overflow the 500-line API cap several times over at flip time,
    // and the old "one final pull" silently truncated that tail (FIXLIST
    // 1.2). Keep going while the last batch was full, or for a few ticks
    // after the flip (the puller can lag the poller by one fetch).
    if (d.status !== lastPulledStatus) {
      lastPulledStatus = d.status;
      terminalTicks = 0;
    } else if (!lastBatchFull) {
      terminalTicks++;
    }
    if (lastBatchFull || terminalTicks < 3) void pullLogs();
  },
  { immediate: true },
);

const logBox = ref<HTMLElement | null>(null);
// Watch length, not the ref: lines.value.push() mutates in place, which a
// non-deep ref watch never sees — so autoscroll never fired on new lines.
watch(
  () => lines.value.length,
  async () => {
    await nextTick();
    if (logBox.value) logBox.value.scrollTop = logBox.value.scrollHeight;
  },
);

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
