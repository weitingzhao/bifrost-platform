#!/usr/bin/env node
/** Wave 4c — p6-escape-hatch static verification. */
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

check('escapehatch package', fs.existsSync(path.join(repoRoot, 'api/internal/escapehatch/service.go')))
check('escape hatch GET route', readRepo('api/internal/server/server.go').includes('/platform/escape-hatch'))
check('escape hatch drill route', readRepo('api/internal/server/server.go').includes('/platform/escape-hatch/drill'))
check('drill persisted under data/escape_hatch', readRepo('api/internal/escapehatch/store.go').includes('"escape_hatch"'))
check('escapeHatchCatalog.ts', fs.existsSync(path.join(root, 'src/lib/architecture/escapeHatchCatalog.ts')))
check('EscapeHatchPanel component', fs.existsSync(path.join(root, 'src/components/architecture/EscapeHatchPanel.tsx')))
check('Platform Release mounts panel', read('src/pages/PlatformReleasePage.tsx').includes('EscapeHatchPanel'))
check('fetchEscapeHatch client', read('src/api/platform.ts').includes('fetchEscapeHatch'))
check('spine p6-escape-hatch done', readRepo('config/ops-context.yaml').includes('id: p6-escape-hatch') && readRepo('config/ops-context.yaml').includes('status: done'))
check('cicdBootstrapCatalog implemented gap', read('src/lib/architecture/cicdBootstrapCatalog.ts').includes('GET /api/v1/platform/escape-hatch'))
check('quarterly interval 90d', read('src/lib/architecture/escapeHatchCatalog.ts').includes('90'))
check('runbook make start step', read('src/lib/architecture/escapeHatchCatalog.ts').includes('make start'))

const pass = checks.filter(c => c.ok).length
const total = checks.length
console.log(`\nWave 4c Escape Hatch (p6-escape-hatch): ${pass}/${total} PASS\n`)
for (const c of checks) {
  console.log(`${c.ok ? '✓' : '✗'} ${c.name}${c.detail ? ` — ${c.detail}` : ''}`)
}
process.exit(pass === total ? 0 : 1)
