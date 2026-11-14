import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';

export default defineConfig({
  plugins: [svelte()],
  base: './',
  build: {
    outDir: 'dist',
  },
  // `npm run dev` talks to a locally running Linager server; change the port here if yours differs.
  server: {
    proxy: {
      '/api': { target: 'http://127.0.0.1:18080', ws: true },
    },
  },
});
