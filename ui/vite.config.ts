/// <reference types="vitest/config" />
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  base: './',
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
  },
})
