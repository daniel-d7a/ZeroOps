<script setup lang="ts">
import { useQuery } from '@tanstack/vue-query';
import { getSystem } from '@/api/admin';
import Badge from '@/components/ui/Badge.vue';
import Card from '@/components/ui/Card.vue';

const ALL_STATUSES = ['queued', 'building', 'built', 'starting', 'live', 'failed', 'superseded'];

const system = useQuery({
  queryKey: ['admin-system'],
  queryFn: getSystem,
  refetchInterval: 10_000, // heartbeats land every 15s (plan §8)
});

function ago(iso: string): string {
  const s = Math.max(0, Math.round((Date.now() - new Date(iso).getTime()) / 1000));
  if (s < 60) return `${s}s ago`;
  return `${Math.floor(s / 60)}m ${s % 60}s ago`;
}

function isStale(iso: string): boolean {
  return Date.now() - new Date(iso).getTime() > 45_000; // 3 missed heartbeats
}

function count(status: string): number {
  return system.data.value?.deployments.find((d) => d.status === status)?.count ?? 0;
}
</script>

<template>
  <div class="flex flex-col gap-4">
    <p v-if="system.isPending.value" class="text-sm text-muted-foreground">Loading…</p>
    <p v-else-if="system.isError.value" class="text-sm text-destructive">Failed to load system info.</p>

    <template v-else>
      <section>
        <h2 class="mb-2 text-sm font-semibold">Workers</h2>
        <p v-if="system.data.value?.workers.length === 0" class="text-sm text-muted-foreground">
          No workers have registered a heartbeat yet.
        </p>
        <ul v-else class="flex flex-col gap-2">
          <li v-for="w in system.data.value?.workers" :key="w.id">
            <Card class="flex items-center gap-3 p-3 text-sm">
              <code class="font-mono font-semibold">{{ w.id }}</code>
              <Badge
                v-for="r in w.roles"
                :key="r"
                class="border-transparent bg-secondary text-secondary-foreground"
              >
                {{ r }}
              </Badge>
              <span class="text-xs text-muted-foreground">node {{ w.nodeId }}</span>
              <span class="flex-1" />
              <span
                class="text-xs"
                :class="isStale(w.lastSeenAt) ? 'font-semibold text-destructive' : 'text-muted-foreground'"
                :title="new Date(w.lastSeenAt).toLocaleString()"
              >
                last seen {{ ago(w.lastSeenAt) }}
              </span>
            </Card>
          </li>
        </ul>
      </section>

      <section>
        <h2 class="mb-2 text-sm font-semibold">Deployments</h2>
        <Card class="flex flex-wrap gap-x-6 gap-y-2 p-3 text-sm">
          <span v-for="s in ALL_STATUSES" :key="s" class="flex items-center gap-2">
            <Badge
              :class="
                s === 'live'
                  ? 'border-transparent bg-emerald-600 text-white'
                  : s === 'failed'
                    ? 'border-transparent bg-destructive text-destructive-foreground'
                    : ''
              "
            >
              {{ s }}
            </Badge>
            <span class="font-semibold tabular-nums">{{ count(s) }}</span>
          </span>
        </Card>
      </section>
    </template>
  </div>
</template>
