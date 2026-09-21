/// <reference types="vitest/config" />
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// Vite is opt-in HMR. Product and journey surfaces serve the bundled build.
export default defineConfig({
  plugins: [react()],
  base: '/',
  server: {
    host: '127.0.0.1',
    port: 5173,
    strictPort: true,
    proxy: {
      '/api': 'http://127.0.0.1:13705',
      '/health': 'http://127.0.0.1:13705',
      '/ws': { target: 'ws://127.0.0.1:13705', ws: true },
    },
  },
  test: {
    environment: 'jsdom',
    // 🎯T801: the suite runs beside a busy fleet (host load average >100).
    // Fixed windows (vitest's 5 s, RTL's 1 s waitFor) measured the host, not
    // the code. Timeouts are ceilings only: a passing wait returns as soon as
    // its condition holds, so generous values cost nothing when idle.
    testTimeout: 60_000,
    hookTimeout: 60_000,
    setupFiles: ['./src/test-setup-timeouts.ts'],
  },
})
