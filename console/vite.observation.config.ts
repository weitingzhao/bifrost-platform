import path from 'node:path'
import { defineConfig } from 'vite'
import base from './vite.config'

/** Isolated entry and dependency cache; shared checkout and other previews remain untouched. */
export default defineConfig({ ...base,
  cacheDir: path.resolve(__dirname, '.observation-cache'),
  build: { outDir: 'dist-observation', rollupOptions: { input: path.resolve(__dirname, 'observation-preview.html') } },
  resolve: { ...base.resolve, dedupe: ['react', 'react-dom'], alias: [
    { find: '@bifrost/ui/city/observationContext', replacement: path.resolve(__dirname, '../../bifrost-ui/src/city/observationContext.ts') },
    { find: '@bifrost/ui/city/observation', replacement: path.resolve(__dirname, '../../bifrost-ui/src/city/observation.ts') },
    ...(Array.isArray(base.resolve?.alias) ? base.resolve.alias : []),
  ] },
})
