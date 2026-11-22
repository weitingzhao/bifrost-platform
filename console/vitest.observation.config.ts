import path from 'node:path'
import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'
export default defineConfig({
  plugins: [react()],
  resolve: { dedupe: ['react', 'react-dom'], alias: [
    { find: /^@bifrost\/ui\/city\/(.*)$/, replacement: path.resolve(__dirname, '../../bifrost-ui/src/city') + '/$1' },
    ...(process.env.COMMUNICATION_MODEL ? [{ find: '@communication-contract', replacement: process.env.COMMUNICATION_MODEL }] : []),
  ] },
  test: { environment: 'jsdom', maxWorkers: 1, include: ['src/observation-preview/surface.test.tsx', ...(process.env.COMMUNICATION_MODEL ? ['src/observation-preview/communication-contract.test.tsx'] : [])] },
})
