import path from 'node:path'
import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

const uiRoot = path.resolve(__dirname, '../../bifrost-ui')
// Default loopback-only. run_platform.py sets PLATFORM_CONSOLE_HOST=0.0.0.0 for LAN.
const consoleHost = (process.env.PLATFORM_CONSOLE_HOST || '127.0.0.1').trim() || '127.0.0.1'
const consolePort = Number(process.env.PLATFORM_CONSOLE_PORT || 5180)

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: [
      { find: '@', replacement: path.resolve(__dirname, './src') },
      // More specific path must come before `@bifrost/ui` (prefix would swallow `/styles`).
      {
        find: '@bifrost/ui/styles',
        replacement: path.join(uiRoot, 'src/styles/bifrost-ui.css'),
      },
      { find: '@bifrost/ui', replacement: path.join(uiRoot, 'src/index.ts') },
    ],
  },
  server: {
    // Bind IPv4 explicitly — bare host can prefer [::1] and break IPv4 clients.
    host: consoleHost,
    port: consolePort,
    strictPort: true,
    fs: {
      allow: [path.resolve(__dirname, '..'), uiRoot],
    },
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:8780',
        changeOrigin: true,
        ws: true,
      },
      '/health': {
        target: 'http://127.0.0.1:8780',
        changeOrigin: true,
      },
    },
  },
})
