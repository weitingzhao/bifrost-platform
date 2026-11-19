import path from 'node:path'
import { defineConfig, mergeConfig } from 'vite'
import base from '../vite.config'

// Builds bench/gen2-page.html into one JS and one CSS file (no chunks), for inline-page.mjs to fold into one page.
//   npx vite build --config bench/vite.gen2-page.config.ts --outDir <dir>
export default mergeConfig(base, defineConfig({
  root: path.resolve(__dirname, '..'),
  base: './',
  build: {
    emptyOutDir: true,
    cssCodeSplit: false,
    assetsInlineLimit: 100_000_000,
    modulePreload: false,
    rollupOptions: { input: path.resolve(__dirname, 'gen2-page.html'), output: { inlineDynamicImports: true } },
  },
}))
