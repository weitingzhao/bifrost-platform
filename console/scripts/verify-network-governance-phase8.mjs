#!/usr/bin/env node
/** Static verification for Network Governance Phase 8 delivery scope. */
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const root = path.join(path.dirname(fileURLToPath(import.meta.url)), '..')
const read = rel => fs.readFileSync(path.join(root, rel), 'utf8')

const checks = []

function check(name, ok, detail) {
  checks.push({ name, ok, detail })
}

const d8 = read('src/lib/architecture/networkGovernancePhase8Delivery.ts')
const ng8Ids = [...d8.matchAll(/id: 'NG8-\d'/g)].map(m => m[0])
check('NG8-1..5 delivery items defined', ng8Ids.length === 5, `${ng8Ids.length}/5`)

const ps = read('src/lib/architecture/networkGovernanceProgramStatus.ts')
const phases = [...ps.matchAll(/id: 'NG\d'/g)].map(m => m[0])
check('Program status strip phases NG1–NG7', phases.length === 7, phases.join(', '))

const bp = read('src/pages/BlueprintPage.tsx')
check('Blueprint mounts NetworkGovernanceProgramStatusStrip', bp.includes('NetworkGovernanceProgramStatusStrip'))
check('Blueprint mounts NetworkGovernancePhase8SignoffPanel', bp.includes('NetworkGovernancePhase8SignoffPanel'))
check('Phase 8 requires prior phases (priorNetworkGovernancePhasesSignedOff)', d8.includes('priorNetworkGovernancePhasesSignedOff'))

for (let i = 1; i <= 7; i++) {
  check(`Blueprint mounts NetworkGovernancePhase${i}SignoffPanel`, bp.includes(`NetworkGovernancePhase${i}SignoffPanel`))
}

const cat = read('src/lib/architecture/blueprintCatalog.ts')
check('Constitution Principle 8 — Network is the ground floor', cat.includes('Network is the ground floor'))
check('CONSOLE_VIEWS includes Network API', /view: 'Network API'/.test(cat))

const nac = read('src/lib/architecture/networkApiContractCatalog.ts')
const routes = [...nac.matchAll(/\/api\/v1\/network[^\s'"]+/g)]
check('networkApiContractCatalog planned routes', routes.length >= 9, `${routes.length} route refs`)

const mcpTools = [...nac.matchAll(/mcpTool: '[^']+'/g)]
check('MCP tool mappings in contract catalog', mcpTools.length >= 5, `${mcpTools.length} tools`)

const proj = read('src/lib/architecture/networkConsoleProjection.ts')
check('Network Health live probe wired', proj.includes('liveProbeNote') && proj.includes('/api/v1/network/status'))

const ng5 = read('src/lib/architecture/networkGovernancePhase5Delivery.ts')
check('NG5 wifi progress text fixed (no 3/5)', !ng5.includes('network-upgrade-wifi 3/5'))
check('NG5 wifi progress 2/5', ng5.includes('network-upgrade-wifi 2/5'))

const pass = checks.filter(c => c.ok).length
const total = checks.length
console.log(`\nNetwork Governance Phase 8 static verification: ${pass}/${total} PASS\n`)
for (const c of checks) {
  console.log(`${c.ok ? '✓' : '✗'} ${c.name}${c.detail ? ` — ${c.detail}` : ''}`)
}
process.exit(pass === total ? 0 : 1)
