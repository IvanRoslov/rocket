/// <reference types="vitest/config" />
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  server: { proxy: { '/v1': { target: 'http://127.0.0.1:4477', ws: true } } },
  build: { emptyOutDir: true },
  test: { environment: 'jsdom', setupFiles: './src/vitest.setup.ts', globals: true },
})
