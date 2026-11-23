import path from 'node:path'
import { defineConfig } from 'vite'
import base from './vite.config'
export default defineConfig({ ...base,
  cacheDir: path.resolve(__dirname, '.observation-access-cache'),
  optimizeDeps: { entries: ['observation-access.html'] },
  resolve: { ...base.resolve, dedupe: ['react', 'react-dom'] },
  build: { outDir: 'dist-observation-access', rollupOptions: { input: path.resolve(__dirname, 'observation-access.html') } },
})
