<script setup lang="ts">
import Button from '@/components/ui/Button.vue';

const props = defineProps<{ total: number; offset: number; limit: number }>();
const emit = defineEmits<{ 'update:offset': [value: number] }>();

function go(delta: number) {
  emit('update:offset', Math.max(0, props.offset + delta));
}
</script>

<template>
  <div v-if="total > limit" class="flex items-center gap-3 text-sm text-muted-foreground">
    <span>
      {{ offset + 1 }}–{{ Math.min(offset + limit, total) }} of {{ total }}
    </span>
    <Button size="sm" variant="outline" :disabled="offset === 0" @click="go(-limit)">Previous</Button>
    <Button size="sm" variant="outline" :disabled="offset + limit >= total" @click="go(limit)">Next</Button>
  </div>
</template>
