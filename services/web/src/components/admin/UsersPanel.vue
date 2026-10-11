<script setup lang="ts">
import { computed, ref } from 'vue';
import { useMutation, useQuery } from '@tanstack/vue-query';
import { ApiError } from '@/api/client';
import { listAdminUsers, setUserStatus, type AdminUser } from '@/api/admin';
import { queryClient } from '@/api/queryClient';
import { useOffsetParam } from '@/composables/useOffsetParam';
import AlertDialog from '@/components/ui/AlertDialog.vue';
import AlertDialogAction from '@/components/ui/AlertDialogAction.vue';
import AlertDialogCancel from '@/components/ui/AlertDialogCancel.vue';
import AlertDialogContent from '@/components/ui/AlertDialogContent.vue';
import AlertDialogDescription from '@/components/ui/AlertDialogDescription.vue';
import AlertDialogFooter from '@/components/ui/AlertDialogFooter.vue';
import AlertDialogHeader from '@/components/ui/AlertDialogHeader.vue';
import AlertDialogTitle from '@/components/ui/AlertDialogTitle.vue';
import Badge from '@/components/ui/Badge.vue';
import Button from '@/components/ui/Button.vue';
import Card from '@/components/ui/Card.vue';
import Pager from '@/components/admin/Pager.vue';

// Offset lives in ?offset= (useOffsetParam) — shareable and reload-proof.
const { offset, setOffset } = useOffsetParam();
const actionError = ref('');
const target = ref<AdminUser | null>(null); // user being enabled/disabled

const me = queryClient.getQueryData<import('@/api/auth').User>(['me']);

const users = useQuery({
  queryKey: computed(() => ['admin-users', offset.value]),
  queryFn: () => listAdminUsers(offset.value),
});

const toggle = useMutation({
  mutationFn: (input: { id: string; status: 'active' | 'disabled' }) => setUserStatus(input.id, input.status),
  onSuccess: () => {
    target.value = null;
    void queryClient.invalidateQueries({ queryKey: ['admin-users'] });
  },
  onError: (e) => {
    target.value = null;
    actionError.value = e instanceof ApiError ? e.message : 'could not update user';
  },
});
</script>

<template>
  <div class="flex flex-col gap-3">
    <p v-if="actionError" class="text-sm text-destructive" role="alert">{{ actionError }}</p>
    <p v-if="users.isPending.value" class="text-sm text-muted-foreground">Loading…</p>
    <p v-else-if="users.isError.value" class="text-sm text-destructive">Failed to load users.</p>
    <template v-else>
      <ul class="flex flex-col gap-2">
        <li v-for="u in users.data.value?.items" :key="u.id">
          <Card class="flex items-center gap-3 p-3 text-sm">
            <span class="font-medium">{{ u.email }}</span>
            <Badge :class="u.role === 'admin' ? 'border-transparent bg-primary text-primary-foreground' : ''">
              {{ u.role }}
            </Badge>
            <Badge
              :class="
                u.status === 'active'
                  ? 'border-transparent bg-emerald-600 text-white'
                  : 'border-transparent bg-destructive text-destructive-foreground'
              "
            >
              {{ u.status }}
            </Badge>
            <span class="text-muted-foreground">{{ u.projectCount }} project{{ u.projectCount === 1 ? '' : 's' }}</span>
            <span class="text-xs text-muted-foreground">{{ new Date(u.createdAt).toLocaleDateString() }}</span>
            <span class="flex-1" />
            <Button
              v-if="u.id !== me?.id"
              size="sm"
              :variant="u.status === 'active' ? 'destructive' : 'outline'"
              @click="actionError = ''; target = u"
            >
              {{ u.status === 'active' ? 'Disable' : 'Enable' }}
            </Button>
          </Card>
        </li>
      </ul>
      <Pager
        :total="users.data.value?.total ?? 0"
        :offset="offset"
        :limit="20"
        @update:offset="setOffset"
      />
    </template>

    <AlertDialog :open="target !== null" @update:open="(v) => !v && (target = null)">
      <AlertDialogContent v-if="target">
        <AlertDialogHeader>
          <AlertDialogTitle>
            {{ target.status === 'active' ? `Disable ${target.email}?` : `Enable ${target.email}?` }}
          </AlertDialogTitle>
          <AlertDialogDescription v-if="target.status === 'active'">
            The account is blocked immediately and all of their sessions are destroyed. Enable it
            again to restore access.
          </AlertDialogDescription>
          <AlertDialogDescription v-else>
            The account can sign in again.
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction
            @click="toggle.mutate({ id: target.id, status: target.status === 'active' ? 'disabled' : 'active' })"
          >
            {{ target.status === 'active' ? 'Disable' : 'Enable' }}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  </div>
</template>
