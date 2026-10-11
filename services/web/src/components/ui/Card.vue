<script setup lang="ts">
import { cn } from '@/lib/utils';

// Vendored shadcn-vue Card (new-york) — flat single card; header/title/
// subcomponents arrive when a view actually needs them.
defineProps<{ class?: string }>();
</script>

<template>
  <div :class="cn('rounded-xl border bg-card text-card-foreground shadow', $props.class)"><slot /></div>
</template>
