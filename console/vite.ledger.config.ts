import path from 'node:path'
import { mergeConfig } from 'vite'
import base from './vite.config'
export default mergeConfig(base, { resolve: { dedupe: ['react', 'react-dom'] }, optimizeDeps: { entries: ['ledger-preview.html'] }, build: { outDir: 'dist-ledger', rollupOptions: { input: path.resolve(__dirname, 'ledger-preview.html') } } })
