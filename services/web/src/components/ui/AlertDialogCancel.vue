<script setup lang="ts">
import { AlertDialogCancel } from 'reka-ui';
import { cn } from '@/lib/utils';

defineProps<{ class?: string }>();
</script>

<template>
  <AlertDialogCancel
    :class="
      cn(
        'inline-flex h-9 items-center justify-center rounded-md border border-input bg-background px-4 text-sm font-medium shadow-sm hover:bg-accent hover:text-accent-foreground',
        $props.class,
      )
    "
  >
    <slot />
  </AlertDialogCancel>
</template>
