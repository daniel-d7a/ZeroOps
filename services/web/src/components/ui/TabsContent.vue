<script setup lang="ts">
import { TabsContent } from 'reka-ui';

defineProps<{ value: string; class?: string }>();
</script>

<template>
  <TabsContent :value="value" :class="class">
    <slot />
  </TabsContent>
</template>
