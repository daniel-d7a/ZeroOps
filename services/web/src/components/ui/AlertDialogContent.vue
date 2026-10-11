<script setup lang="ts">
import { AlertDialogContent, AlertDialogOverlay } from 'reka-ui';

// Vendored shadcn-vue AlertDialogContent (new-york) — overlay + portaled
// panel; reka handles focus trap, Esc and aria wiring.
</script>

<template>
  <AlertDialogOverlay class="fixed inset-0 z-50 bg-black/80" />
  <AlertDialogContent
    class="fixed left-1/2 top-1/2 z-50 grid w-full max-w-lg -translate-x-1/2 -translate-y-1/2 gap-4 border bg-background p-6 shadow-lg duration-200 sm:rounded-lg"
  >
    <slot />
  </AlertDialogContent>
</template>
