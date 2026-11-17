import path from 'node:path'
import { defineConfig, mergeConfig } from 'vite'
import base from '../vite.config'

// The bench's dev server: the Console's config, on its own port, with a
// polling watcher (in an agent worktree the native watcher missed edits).
//   npx vite --config bench/vite.bench.config.ts   → http://127.0.0.1:5191/bench/nerve.html
export default mergeConfig(base, defineConfig({
  root: path.resolve(__dirname, '..'),
  server: { port: 5191, strictPort: true, watch: { usePolling: true, interval: 300 } },
}))
