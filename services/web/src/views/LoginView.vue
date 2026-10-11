<script setup lang="ts">
import { ref } from 'vue';
import { useField, useForm } from 'vee-validate';
import { toTypedSchema } from '@vee-validate/zod';
import * as z from 'zod';
import { useRoute, useRouter } from 'vue-router';
import { ApiError } from '@/api/client';
import { useAuthStore } from '@/stores/auth';
import Button from '@/components/ui/Button.vue';
import Card from '@/components/ui/Card.vue';
import Input from '@/components/ui/Input.vue';
import Label from '@/components/ui/Label.vue';

const auth = useAuthStore();
const route = useRoute();
const router = useRouter();
const error = ref('');

// vee-validate + zod: one schema is the validation AND the form types.
const schema = toTypedSchema(
  z.object({
    email: z.string().min(1, 'Email is required').email('Enter a valid email'),
    password: z.string().min(1, 'Password is required'),
  }),
);
const { handleSubmit, errors, isSubmitting } = useForm({ validationSchema: schema });
const email = useField<string>('email');
const password = useField<string>('password');

const submit = handleSubmit(async (values) => {
  error.value = '';
  try {
    await auth.login(values.email, values.password);
    const redirect = typeof route.query.redirect === 'string' ? route.query.redirect : '/';
    await router.replace(redirect);
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : 'login failed';
  }
});
</script>

<template>
  <main class="mx-auto mt-24 w-full max-w-sm px-4">
    <Card class="p-6">
      <h1 class="mb-4 text-lg font-semibold">Sign in</h1>
      <form class="flex flex-col gap-4" novalidate @submit="submit">
        <div class="flex flex-col gap-2">
          <Label for="email">Email</Label>
          <Input id="email" type="email" autocomplete="email" placeholder="you@example.com" v-bind="email" />
          <p v-if="errors.email" class="text-sm text-destructive">{{ errors.email }}</p>
        </div>
        <div class="flex flex-col gap-2">
          <Label for="password">Password</Label>
          <Input id="password" type="password" autocomplete="current-password" v-bind="password" />
          <p v-if="errors.password" class="text-sm text-destructive">{{ errors.password }}</p>
        </div>
        <p v-if="error" class="text-sm text-destructive" role="alert">{{ error }}</p>
        <Button type="submit" :disabled="isSubmitting">{{ isSubmitting ? 'Signing in…' : 'Sign in' }}</Button>
      </form>
    </Card>
  </main>
</template>
