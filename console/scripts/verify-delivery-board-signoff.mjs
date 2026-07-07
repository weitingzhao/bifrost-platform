#!/usr/bin/env node
/** Static verification: Delivery Board API sign-off (no legacy localStorage UI). */
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

const board = read('src/pages/DeliveryBoardPage.tsx')
check('Delivery Board — no legacy migration banner', !board.includes('LegacySignoffMigrationBanner'))

const ibCat = read('src/lib/architecture/ibGatewayPluginCatalog.ts')
check('IB Gateway catalog — no legacy sign-off panel refs', !ibCat.includes('sign-off panel'))
check('IB Gateway catalog — Delivery Board sign-off', ibCat.includes('Delivery Board'))

const tibmCat = read('src/lib/architecture/tradeIbClientMigrationCatalog.ts')
check('TIBM catalog — no Phase N panel sign-off', !/Phase \d panel signed/.test(tibmCat))

const agentCat = read('src/lib/architecture/agentProtocolCatalog.ts')
check('Agent Protocol — Delivery Board mission-signal closure', agentCat.includes('Delivery Board · mission-signal'))

const tibmYaml = readRepo('config/programs/trade-ib-client-migration.yaml')
check('TIBM YAML — no legacy panel acceptance', !tibmYaml.includes('panel signed in Console'))

const programsApi = read('src/api/programs.ts')
check('signoffProgramPhase accepts signed_off_at', programsApi.includes('signed_off_at'))

const pass = checks.filter(c => c.ok).length
const total = checks.length
console.log(`\nDelivery Board sign-off (API-only): ${pass}/${total} PASS\n`)
for (const c of checks) {
  console.log(`${c.ok ? '✓' : '✗'} ${c.name}${c.detail ? ` — ${c.detail}` : ''}`)
}
process.exit(pass === total ? 0 : 1)
