<script setup lang="ts">
import { api, ApiError } from '@/api/client';
import Button from '@/components/ui/Button.vue';
import Card from '@/components/ui/Card.vue';
import Input from '@/components/ui/Input.vue';
import Label from '@/components/ui/Label.vue';
import { toTypedSchema } from '@vee-validate/zod';
import { useField, useForm } from 'vee-validate';
import { ref } from 'vue';
import { RouterLink, useRouter } from 'vue-router';
import * as z from 'zod';

const router = useRouter();
const error = ref('');
const done = ref(false);

// Mirrors API RegisterDto: email + password 8..128 (plan §5).
const schema = toTypedSchema(
  z
    .object({
      email: z.string().min(1, 'Email is required').email('Enter a valid email'),
      password: z.string().min(8, 'At least 8 characters').max(128, 'At most 128 characters'),
      confirm: z.string().min(1, 'Confirm your password'),
    })
    .refine((v) => v.password === v.confirm, { message: 'Passwords do not match', path: ['confirm'] }),
);
const { handleSubmit, errors, isSubmitting } = useForm({ validationSchema: schema });
const email = useField<string>('email');
const password = useField<string>('password');
const confirm = useField<string>('confirm');

const submit = handleSubmit(async (values) => {
  error.value = '';
  try {
    await api('/auth/register', { json: { email: values.email, password: values.password } });
    done.value = true;
    // Register does not create a session (plan §5) — sign in next.
    void router.replace('/login');
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : 'registration failed';
  }
});
</script>

<template>
  <main class="mx-auto mt-24 w-full max-w-sm px-4">
    <Card class="p-6">
      <h1 class="mb-1 text-lg font-semibold">Create account</h1>
      <p class="mb-4 text-sm text-muted-foreground">
        Already registered?
        <RouterLink class="underline" to="/login">Sign in</RouterLink>
      </p>
      <form v-if="!done" class="flex flex-col gap-4" novalidate @submit="submit">
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
            autocomplete="new-password"
            :value="password.value.value"
            @input="password.handleChange"
            @blur="password.handleBlur"
          />
          <p v-if="errors.password" class="text-sm text-destructive">{{ errors.password }}</p>
        </div>
        <div class="flex flex-col gap-2">
          <Label for="confirm">Confirm password</Label>
          <Input
            id="confirm"
            type="password"
            autocomplete="new-password"
            :value="confirm.value.value"
            @input="confirm.handleChange"
            @blur="confirm.handleBlur"
          />
          <p v-if="errors.confirm" class="text-sm text-destructive">{{ errors.confirm }}</p>
        </div>
        <p v-if="error" class="text-sm text-destructive" role="alert">{{ error }}</p>
        <Button type="submit" :disabled="isSubmitting">{{ isSubmitting ? 'Creating…' : 'Create account' }}</Button>
      </form>
      <p v-else class="text-sm text-muted-foreground">Account created — redirecting to sign in…</p>
    </Card>
  </main>
</template>
