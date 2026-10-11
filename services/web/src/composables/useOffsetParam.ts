import { computed } from 'vue';
import { useRoute, useRouter } from 'vue-router';

/** Pagination offset as `?offset=` in the URL (shareable, survives reload,
 *  TanStack caches each page per query key). 0 is omitted for clean URLs. */
export function useOffsetParam() {
  const route = useRoute();
  const router = useRouter();
  const offset = computed(() => Math.max(0, Number(route.query.offset) || 0));
  function setOffset(value: number) {
    void router.replace({ query: { ...route.query, offset: value ? String(value) : undefined } });
  }
  return { offset, setOffset };
}
