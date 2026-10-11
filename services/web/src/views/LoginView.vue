<script setup lang="ts">
import { ref } from 'vue';
import { RouterLink, useRoute, useRouter } from 'vue-router';
import { useMutation } from '@tanstack/vue-query';
import { useField, useForm } from 'vee-validate';
import { toTypedSchema } from '@vee-validate/zod';
import * as z from 'zod';
import { ApiError, setCsrfToken } from '@/api/client';
import { login, type User } from '@/api/auth';
import { queryClient } from '@/api/queryClient';
import Button from '@/components/ui/Button.vue';
import Card from '@/components/ui/Card.vue';
import Input from '@/components/ui/Input.vue';
import Label from '@/components/ui/Label.vue';

const route = useRoute();
const router = useRouter();
const error = ref('');

const schema = toTypedSchema(
  z.object({
    email: z.string().min(1, 'Email is required').email('Enter a valid email'),
    password: z.string().min(1, 'Password is required'),
  }),
);
const { handleSubmit, errors } = useForm({ validationSchema: schema });
const email = useField<string>('email');
const password = useField<string>('password');

const loginMutation = useMutation({
  mutationFn: ({ email, password }: { email: string; password: string }) => login(email, password),
  onSuccess: (user: User) => {
    setCsrfToken(user.csrfToken ?? null);
    queryClient.setQueryData(['me'], user);
    const redirect = typeof route.query.redirect === 'string' ? route.query.redirect : '/';
    void router.replace(redirect);
  },
  onError: (e) => {
    error.value = e instanceof ApiError ? e.message : 'login failed';
  },
});

const submit = handleSubmit((values) => {
  error.value = '';
  loginMutation.mutate(values);
});
</script>

<template>
  <main class="mx-auto mt-24 w-full max-w-sm px-4">
    <Card class="p-6">
      <h1 class="mb-1 text-lg font-semibold">Sign in</h1>
      <p class="mb-4 text-sm text-muted-foreground">
        No account?
        <RouterLink class="underline" to="/register">Create one</RouterLink>
      </p>
      <form class="flex flex-col gap-4" novalidate @submit="submit">
        <div class="flex flex-col gap-2">
          <Label for="email">Email</Label>
          <Input
            id="email"
            type="email"
            autocomplete="email"
            placeholder="you@example.com"
            :value="email.value.value"
            @input="email.handleChange"
            @blur="email.handleBlur"
          />
          <p v-if="errors.email" class="text-sm text-destructive">{{ errors.email }}</p>
        </div>
        <div class="flex flex-col gap-2">
          <Label for="password">Password</Label>
          <Input
            id="password"
            type="password"
            autocomplete="current-password"
            :value="password.value.value"
            @input="password.handleChange"
            @blur="password.handleBlur"
          />
          <p v-if="errors.password" class="text-sm text-destructive">{{ errors.password }}</p>
        </div>
        <p v-if="error" class="text-sm text-destructive" role="alert">{{ error }}</p>
        <Button type="submit" :disabled="loginMutation.isPending.value">
          {{ loginMutation.isPending.value ? 'Signing in…' : 'Sign in' }}
        </Button>
      </form>
    </Card>
  </main>
</template>
