import { QueryClient } from '@tanstack/vue-query';

// One client for the app. No retries: the platform is local and errors should
// surface immediately (same philosophy as the worker's transient ladder —
// this UI is not the retry layer). Focus refetch would yank project lists
// around while the user is reading them.
export const queryClient = new QueryClient({
  defaultOptions: {
    queries: { retry: false, staleTime: 30_000, refetchOnWindowFocus: false },
    mutations: { retry: false },
  },
});
