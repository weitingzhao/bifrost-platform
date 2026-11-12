import path from 'node:path'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vitest/config'

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: [{ find: '@', replacement: path.resolve(__dirname, './src') }],
  },
  test: {
    // Bound jsdom concurrency so loaded CI workers do not time out healthy tests.
    maxWorkers: 2,
    testTimeout: 15_000,
    environment: 'jsdom',
    globals: true,
    css: false,
    // The real clock is moved ten years ahead for every test file (TD-318).
    // webStorage: jsdom's storages on a local Node newer than CI's (.nvmrc).
    setupFiles: ['./src/test/webStorage.ts', './src/test/clockShift.ts'],
  },
})
