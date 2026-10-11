<script setup lang="ts">
import { useRouter } from 'vue-router';
import { useAuthStore } from '@/stores/auth';

const auth = useAuthStore();
const router = useRouter();

async function logout() {
  await auth.logout();
  await router.replace('/login');
}
</script>

<template>
  <main class="page">
    <header class="row">
      <h1>Projects</h1>
      <div class="spacer" />
      <span class="who">{{ auth.user?.email }}</span>
      <button class="ghost" @click="logout">Sign out</button>
    </header>
    <p class="hint">Project list arrives in M6.2 — you are signed in.</p>
  </main>
</template>

<style>
.page {
  max-width: 56rem;
  margin: 3rem auto 0;
  padding: 0 1.5rem;
}
.row {
  display: flex;
  align-items: baseline;
  gap: 0.75rem;
}
.spacer {
  flex: 1;
}
h1 {
  margin: 0;
  font-size: 1.4rem;
}
.who {
  color: #555;
  font-size: 0.85rem;
}
button.ghost {
  border: 1px solid #ccc;
  background: #fff;
  border-radius: 6px;
  padding: 0.35rem 0.7rem;
  cursor: pointer;
  font-size: 0.85rem;
}
.hint {
  color: #666;
  margin-top: 1.5rem;
}
</style>
