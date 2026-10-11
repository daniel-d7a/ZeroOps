import { VueQueryPlugin } from '@tanstack/vue-query';
import { createPinia } from 'pinia';
import { createApp } from 'vue';
import App from './App.vue';
import { queryClient } from './api/queryClient';
import { router } from './router';
import './assets/main.css';

createApp(App).use(createPinia()).use(VueQueryPlugin, { queryClient }).use(router).mount('#app');
