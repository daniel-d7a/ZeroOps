<script setup lang="ts">
import { ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { ApiError } from '@/api/client';
import { useAuthStore } from '@/stores/auth';

const auth = useAuthStore();
const route = useRoute();
const router = useRouter();

const email = ref('');
const password = ref('');
const error = ref('');
const busy = ref(false);

async function submit() {
  error.value = '';
  busy.value = true;
  try {
    await auth.login(email.value, password.value);
    const redirect = typeof route.query.redirect === 'string' ? route.query.redirect : '/';
    await router.replace(redirect);
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : 'login failed';
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <main class="page">
    <form class="card" @submit.prevent="submit">
      <h1>Sign in</h1>
      <label>
        Email
        <input v-model="email" type="email" name="email" autocomplete="email" required />
      </label>
      <label>
        Password
        <input v-model="password" type="password" name="password" autocomplete="current-password" required />
      </label>
      <p v-if="error" class="error" role="alert">{{ error }}</p>
      <button type="submit" :disabled="busy">{{ busy ? 'Signing in…' : 'Sign in' }}</button>
    </form>
  </main>
</template>

<style>
body {
  margin: 0;
  font-family: ui-sans-serif, system-ui, sans-serif;
  background: #fafafa;
  color: #111;
}
.page {
  max-width: 24rem;
  margin: 14vh auto 0;
  padding: 0 1.5rem;
}
.card {
  display: flex;
  flex-direction: column;
  gap: 0.9rem;
  background: #fff;
  border: 1px solid #e5e5e5;
  border-radius: 10px;
  padding: 1.5rem;
}
h1 {
  margin: 0;
  font-size: 1.3rem;
}
label {
  display: flex;
  flex-direction: column;
  gap: 0.3rem;
  font-size: 0.85rem;
  color: #444;
}
input {
  padding: 0.5rem 0.6rem;
  border: 1px solid #ccc;
  border-radius: 6px;
  font-size: 0.95rem;
}
button {
  padding: 0.55rem;
  border: 0;
  border-radius: 6px;
  background: #111;
  color: #fff;
  font-size: 0.95rem;
  cursor: pointer;
}
button:disabled {
  opacity: 0.5;
}
.error {
  margin: 0;
  color: #b00020;
  font-size: 0.85rem;
}
</style>
