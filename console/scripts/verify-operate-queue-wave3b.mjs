#!/usr/bin/env node
/** Wave 3b — D11 Operate Queue static verification. */
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const root = path.join(path.dirname(fileURLToPath(import.meta.url)), '..')
const repoRoot = path.join(root, '..')
const read = rel => fs.readFileSync(path.join(root, rel), 'utf8')
const readRepo = rel => fs.readFileSync(path.join(repoRoot, rel), 'utf8')

const checks = []

function check(name, ok, detail) {
  checks.push({ name, ok, detail })
}

check('operatequeue store exists', fs.existsSync(path.join(repoRoot, 'api/internal/operatequeue/store.go')))
check('operatequeue handler routes', readRepo('api/internal/server/server.go').includes('/operate/queue'))
check('MCP catalog get_operate_queue', readRepo('api/internal/mcp/catalog.go').includes('get_operate_queue'))
check('MCP server get_operate_queue', fs.readFileSync(path.join(repoRoot, 'mcp/platform/src/index.ts'), 'utf8').includes('get_operate_queue'))
check('useOperateQueue hook', fs.existsSync(path.join(root, 'src/hooks/useOperateQueue.ts')))
check('Control Room OperateQueueStrip', read('src/pages/ControlRoomPage.tsx').includes('OperateQueueStrip'))
check('Ops Desk operate handoff panel', read('src/components/agent/AgentDeskSessionOpsPanels.tsx').includes('OperateQueueHandoffPanel'))
check('program hand-off path retired (no devagent package)', !fs.existsSync(path.join(repoRoot, 'api/internal/devagent')))
check('queue persisted under data/operate', readRepo('api/internal/operatequeue/store.go').includes('"operate"') && readRepo('api/internal/operatequeue/store.go').includes('"queue.json"'))
check('no spine tracks.operate write in operatequeue', !readRepo('api/internal/operatequeue/store.go').includes('tracks.operate'))
check('operate queue close route', readRepo('api/internal/server/server.go').includes('/operate/queue/{id}/close'))
check('store Close method', readRepo('api/internal/operatequeue/store.go').includes('func (s *Store) Close'))
check('MCP close_operate_queue_item', readRepo('api/internal/mcp/catalog.go').includes('close_operate_queue_item'))
check('close API client', read('src/api/operateQueue.ts').includes('closeOperateQueueItem'))
check('Control Room Resolve button', read('src/components/control-room/OperateQueueStrip.tsx').includes('Resolve'))

const pass = checks.filter(c => c.ok).length
const total = checks.length
console.log(`\nWave 3b Operate Queue (D11): ${pass}/${total} PASS\n`)
for (const c of checks) {
  console.log(`${c.ok ? '✓' : '✗'} ${c.name}${c.detail ? ` — ${c.detail}` : ''}`)
}
process.exit(pass === total ? 0 : 1)
