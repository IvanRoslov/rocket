/// <reference types="vitest/config" />
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
export default defineConfig({
 base: '/creative/', plugins: [react()],
 server: { proxy: { '/v1': 'http://127.0.0.1:4477', '/login': 'http://127.0.0.1:4477' } },
 test: { environment: 'jsdom', setupFiles: './src/testSetup.ts', globals: true },
})
