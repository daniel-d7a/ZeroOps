<script setup lang="ts">
import { TabsRoot } from 'reka-ui';

// Vendored shadcn-vue Tabs root (new-york). Uncontrolled via defaultValue,
// or controlled with v-model when the active tab should live in the URL.
defineProps<{ defaultValue?: string; modelValue?: string; class?: string }>();
const emit = defineEmits<{ 'update:modelValue': [value: string] }>();
</script>

<template>
  <TabsRoot
    :default-value="defaultValue"
    :model-value="modelValue"
    :class="class"
    @update:model-value="emit('update:modelValue', $event)"
  >
    <slot />
  </TabsRoot>
</template>
