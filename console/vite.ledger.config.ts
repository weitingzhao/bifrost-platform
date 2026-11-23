import { mergeConfig } from 'vite'
import base from './vite.config'
export default mergeConfig(base, { resolve: { dedupe: ['react', 'react-dom'] }, optimizeDeps: { entries: ['ledger-preview.html'] } })
