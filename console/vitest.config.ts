import path from 'node:path'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vitest/config'

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: [{ find: '@', replacement: path.resolve(__dirname, './src') }],
  },
  test: {
    environment: 'jsdom',
    globals: true,
    css: false,
    // The real clock is moved ten years ahead for every test file (TD-318).
    setupFiles: ['./src/test/clockShift.ts'],
  },
})
