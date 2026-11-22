/// <reference types="vitest/config" />
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// In dev, Vite serves the UI on :5174 and forwards API calls to the Go server
// on :7171. In production the Go server serves web/dist itself, so the code
// always uses relative URLs like "/api/v1/traces".
const API = process.env.LOUPE_API ?? 'http://127.0.0.1:7171'

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5174,
    strictPort: true,
    proxy: {
      '/api': { target: API, changeOrigin: true },
      '/healthz': { target: API, changeOrigin: true },
    },
  },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./src/test/setup.ts'],
    include: ['src/**/*.test.{ts,tsx}'],
  },
})
