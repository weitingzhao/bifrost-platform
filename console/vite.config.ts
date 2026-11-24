import path from 'node:path'
import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

const uiRoot = path.resolve(__dirname, '../../bifrost-ui')
// Default loopback-only. run_platform.py sets PLATFORM_CONSOLE_HOST=0.0.0.0 for LAN.
const consoleHost = (process.env.PLATFORM_CONSOLE_HOST || '127.0.0.1').trim() || '127.0.0.1'
const consolePort = Number(process.env.PLATFORM_CONSOLE_PORT || 5180)
const apiTarget = (process.env.PLATFORM_API_TARGET || 'http://127.0.0.1:8780').trim()

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: [
      { find: '@', replacement: path.resolve(__dirname, './src') },
      // More specific path must come before `@bifrost/ui` (prefix would swallow `/styles`).
      {
        find: '@bifrost/ui/styles',
        replacement: path.join(uiRoot, 'dist/styles/bifrost-ui.css'),
      },
      // The City world (2026-11-20, moved into @bifrost/ui): its entry and its modules, before the bare name.
      { find: /^@bifrost\/ui\/city$/, replacement: path.join(uiRoot, 'dist/city/index.js') },
      { find: /^@bifrost\/ui\/city\/(.*)$/, replacement: path.join(uiRoot, 'dist/city/$1') },
      { find: '@bifrost/ui/communication', replacement: path.join(uiRoot, 'dist/communication/index.js') },
      { find: '@bifrost/ui', replacement: path.join(uiRoot, 'dist/index.js') },
    ],
  },
  // Retired City benches are not application entrypoints; do not eagerly scan them.
  optimizeDeps: { entries: ['index.html', 'communication-preview.html', 'observation-preview.html'] },
  server: {
    // Bind IPv4 explicitly — bare host can prefer [::1] and break IPv4 clients.
    host: consoleHost,
    port: consolePort,
    strictPort: true,
    fs: {
      allow: [path.resolve(__dirname, '..'), uiRoot],
    },
    proxy: {
      // Local owner-exec, when one is running (OWNER_EXEC_TARGET=http://127.0.0.1:8787). Listed first: it is the longer prefix.
      ...(process.env.OWNER_EXEC_TARGET ? { '/api/owner-exec': { target: process.env.OWNER_EXEC_TARGET, changeOrigin: true } } : {}),
      '/api': {
        target: apiTarget,
        changeOrigin: true,
        ws: true,
      },
      // The engine backend, when one is running locally (ENGINE_TARGET=http://127.0.0.1:8790); in a cluster it is served at /engine.
      ...(process.env.ENGINE_TARGET ? { '/engine': { target: process.env.ENGINE_TARGET, changeOrigin: true } } : {}),
      '/health': {
        target: apiTarget,
        changeOrigin: true,
      },
    },
  },
})
