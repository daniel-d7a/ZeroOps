<script setup lang="ts">
import { TabsRoot } from 'reka-ui';

// Vendored shadcn-vue Tabs root (new-york) — uncontrolled by default
// (active tab is view-local state, not a URL).
defineProps<{ defaultValue?: string; class?: string }>();
</script>

<template>
  <TabsRoot :default-value="defaultValue" :class="class">
    <slot />
  </TabsRoot>
</template>
