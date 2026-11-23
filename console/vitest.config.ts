import path from 'node:path'
import { createRequire } from 'node:module'
const resolveDependency = createRequire(import.meta.url).resolve
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vitest/config'

export default defineConfig({
  plugins: [react()],
  resolve: {
    dedupe: ['react', 'react-dom'],
    alias: [{ find: 'phaser', replacement: resolveDependency('phaser') }, { find: /^@bifrost\/ui\/city\/(.*)$/, replacement: path.resolve(__dirname, '../../bifrost-ui/dist/city') + '/$1' }, { find: 'lucide-react', replacement: resolveDependency('lucide-react') }, { find: '@', replacement: path.resolve(__dirname, './src') }, { find: '@bifrost/ui/communication', replacement: path.resolve(__dirname, '../../bifrost-ui/dist/communication/index.js') }],
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
