<script setup lang="ts">
import { AlertDialogRoot } from 'reka-ui';

// Vendored shadcn-vue AlertDialog root (new-york) — controlled `open` so the
// caller decides when the dialog shows (confirmation flow in M6.3).
defineProps<{ open?: boolean }>();
const emit = defineEmits<{ 'update:open': [open: boolean] }>();
</script>

<template>
  <AlertDialogRoot :open="open" @update:open="emit('update:open', $event)">
    <slot />
  </AlertDialogRoot>
</template>
