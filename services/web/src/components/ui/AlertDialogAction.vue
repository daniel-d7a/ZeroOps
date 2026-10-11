<script setup lang="ts">
import { AlertDialogAction } from 'reka-ui';
import { cn } from '@/lib/utils';

defineProps<{ class?: string }>();
</script>

<template>
  <AlertDialogAction
    :class="
      cn(
        'inline-flex h-9 items-center justify-center rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground shadow hover:bg-primary/90',
        $props.class,
      )
    "
  >
    <slot />
  </AlertDialogAction>
</template>
