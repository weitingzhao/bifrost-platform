#!/usr/bin/env node
/** Static verification for Network Governance delivery on unified Delivery Board. */
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

const panels = read('src/components/delivery/DeliveryBoardProgramPanels.tsx')
check('Delivery Board uses ProgramDetailView', panels.includes('ProgramDetailView'))
check('Legacy SignoffPanel removed from Delivery Board', !panels.includes('SignoffPanel'))

const ngYaml = readRepo('config/programs/network-governance.yaml')
check('network-governance program YAML exists', ngYaml.includes('id: network-governance'))
check('network-governance API sign-off mechanism', ngYaml.includes('sign_off_mechanism: api'))

const cat = read('src/lib/architecture/blueprintCatalog.ts')
check('Constitution Principle 8 — Network is the ground floor', cat.includes('Network is the ground floor'))
check('CONSOLE_VIEWS includes Network Health catalog refs', cat.includes('networkUpgradeCatalog.ts') && cat.includes('networkApiContractCatalog.ts'))

const nac = read('src/lib/architecture/networkApiContractCatalog.ts')
const routes = [...nac.matchAll(/\/api\/v1\/network[^\s'"]+/g)]
check('networkApiContractCatalog planned routes', routes.length >= 9, `${routes.length} route refs`)

const mcpTools = [...nac.matchAll(/mcpTool: '[^']+'/g)]
check('MCP tool mappings in contract catalog', mcpTools.length >= 5, `${mcpTools.length} tools`)

const proj = read('src/lib/architecture/networkConsoleProjection.ts')
check('Network Health live probe wired', proj.includes('liveProbeNote') && proj.includes('/api/v1/network/status'))

const pass = checks.filter(c => c.ok).length
const total = checks.length
console.log(`\nNetwork Governance Delivery Board verification: ${pass}/${total} PASS\n`)
for (const c of checks) {
  console.log(`${c.ok ? '✓' : '✗'} ${c.name}${c.detail ? ` — ${c.detail}` : ''}`)
}
process.exit(pass === total ? 0 : 1)
