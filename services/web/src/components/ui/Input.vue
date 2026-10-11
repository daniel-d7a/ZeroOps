<script setup lang="ts">
import { cn } from '@/lib/utils';

// Vendored shadcn-vue Input (new-york).
defineProps<{ class?: string; type?: string; placeholder?: string; disabled?: boolean; autocomplete?: string }>();
</script>

<template>
  <input
    :type="type"
    :placeholder="placeholder"
    :disabled="disabled"
    :autocomplete="autocomplete"
    :class="
      cn(
        'flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm shadow-sm transition-colors placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50',
        $props.class,
      )
    "
    v-bind="$attrs"
  />
</template>
