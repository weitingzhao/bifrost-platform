#!/usr/bin/env node
/**
 * Three Desks frontend verify — nav + task mode + Analysis routes.
 * Insights API is required; readiness probe stays soft if platform-api is down.
 */
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const root = path.join(path.dirname(fileURLToPath(import.meta.url)), '..')
const API = process.env.PLATFORM_API_URL ?? 'http://127.0.0.1:8780'

const checks = []

function check(name, ok, detail) {
  checks.push({ name, ok, detail, soft: false })
}

function softCheck(name, ok, detail) {
  checks.push({ name, ok, detail, soft: true })
}

const read = rel => fs.readFileSync(path.join(root, rel), 'utf8')

const taskMode = read('src/lib/task-mode/taskModeCatalog.ts')
check('taskModeCatalog defines ops mode', /id:\s*'ops'/.test(taskMode))
check('taskModeCatalog defines analysis mode', /id:\s*'analysis'/.test(taskMode))
check(
  'analysis loopArchetype is analysis',
  /id:\s*'analysis'[\s\S]*?loopArchetype:\s*'analysis'/.test(taskMode),
)
check('taskModeCatalog has no standalone daily-ops id', !/id:\s*'daily-ops'/.test(taskMode))
check('taskModeCatalog has no standalone mission-launch id', !/id:\s*'mission-launch'/.test(taskMode))
check("legacy alias daily-ops → ops", taskMode.includes("'daily-ops': 'ops'"))
check("legacy alias mission-launch → ops", taskMode.includes("'mission-launch': 'ops'"))
check("legacy alias patrol → ops", /patrol:\s*'ops'/.test(taskMode) || taskMode.includes("patrol: 'ops'"))
check('ops landingTab is task-cc', /id:\s*'ops'[\s\S]*?landingTab:\s*'task-cc'/.test(taskMode))
check(
  'analysis landingTab is analysis-workspace',
  /id:\s*'analysis'[\s\S]*?landingTab:\s*'analysis-workspace'/.test(taskMode),
)
check('analysis includeTabs has insight-log', taskMode.includes("'insight-log'"))
check('analysis includeTabs has hermes-status', taskMode.includes("'hermes-status'"))

const types = read('src/lib/task-mode/types.ts')
check(
  'TaskModeId is system | build | ops | analysis',
  types.includes("'system'") &&
    types.includes("'build'") &&
    types.includes("'ops'") &&
    types.includes("'analysis'"),
)

const nav = read('src/lib/consoleNavConfig.ts')
check('consoleNav queue tab', nav.includes("id: 'queue'"))
check('consoleNav Analysis Workspace', nav.includes("id: 'analysis-workspace'"))
check('consoleNav Insight Log', nav.includes("id: 'insight-log'"))
check('consoleNav Hermes Status', nav.includes("id: 'hermes-status'"))
check('consoleNav Ops Desk has 6 workspace items', ENGINEER_WORKSPACE_COUNT(nav) === 6)
check('consoleNav Analysis Desk has 3 profile items', ENGINEER_PROFILE_COUNT(nav) === 3)
check('autonomous-skills label is Patrol', /id:\s*'autonomous-skills',\s*label:\s*'Patrol'/.test(nav))

function ENGINEER_WORKSPACE_COUNT(src) {
  const m = src.match(/export const ENGINEER_WORKSPACE_ITEMS[\s\S]*?^]/m)
  if (m == null) return -1
  return (m[0].match(/id:/g) ?? []).length
}

function ENGINEER_PROFILE_COUNT(src) {
  const m = src.match(/export const ENGINEER_PROFILE_ITEMS[\s\S]*?^]/m)
  if (m == null) return -1
  return (m[0].match(/id:/g) ?? []).length
}

const partner = read('src/components/shell/PartnerStrip.tsx')
check('PartnerStrip Build Desk label', partner.includes('Build Desk'))
check('PartnerStrip Ops Desk label', partner.includes('Ops Desk'))
check('PartnerStrip Analysis Desk label', partner.includes('Analysis Desk'))
check('PartnerStrip collapsed trigger Ops & Analysis', partner.includes('Ops & Analysis'))

const consolePage = read('src/pages/ConsolePage.tsx')
check('ConsolePage hash alias agent-desk → queue', consolePage.includes("'agent-desk': 'queue'"))
check('ConsolePage renders AnalysisWorkspacePage', consolePage.includes('AnalysisWorkspacePage'))
check('ConsolePage renders InsightLogPage', consolePage.includes('InsightLogPage'))
check('ConsolePage renders HermesStatusPage', consolePage.includes('HermesStatusPage'))
check('ConsolePage queue title', consolePage.includes("queue: 'Queue'"))

const hermesApi = read('src/api/hermes.ts')
check('hermes insights client', hermesApi.includes('/api/v1/hermes/insights'))
check('hermes run-first-task client', hermesApi.includes('/api/v1/hermes/run-first-task'))
check('HERMES_CHAT_UI_URL', hermesApi.includes('http://192.168.10.50:9119/chat'))

const protocol = read('src/lib/architecture/agentProtocolCatalog.ts')
check('Agent Protocol Three Desks', protocol.includes('Three Desks'))
check('Agent Protocol PATROL distinct from Hermes', protocol.includes('distinctFrom') || protocol.includes('Distinct from Hermes'))
check('Agent Protocol HERMES_ANALYSIS_DESK', protocol.includes('HERMES_ANALYSIS_DESK'))
check('Agent Protocol D10 analysis read-only', protocol.includes('Analysis is read-only. No trading actuation.'))

const blueprint = read('src/lib/architecture/blueprintCatalog.ts')
check('Blueprint Three Desks modes', blueprint.includes("'analysis'") && blueprint.includes("'ops'"))
check('Blueprint Three Desks criterion', blueprint.includes('Three Desks'))

const design = read('src/lib/standards/designSystemCatalog.ts')
check('Design system Three Desks labels', design.includes('Build Desk / Ops Desk / Analysis Desk'))

const compute = read('src/lib/architecture/aiComputeStrategyCatalog.ts')
check('AI Compute ANALYSIS_DESK_COMPUTE_NOTE', compute.includes('ANALYSIS_DESK_COMPUTE_NOTE'))
check('AI Compute PATROL_COMPUTE_NOTE', compute.includes('PATROL_COMPUTE_NOTE'))

const visual = read('src/lib/task-mode/taskModeVisual.ts')
check('ops visual shortLabel Ops', visual.includes("shortLabel: 'Ops'"))
check('analysis visual BrainCircuit', visual.includes('BrainCircuit'))

let apiReachable = false
try {
  const health = await fetch(`${API}/api/v1/agent/hermes/readiness`, { signal: AbortSignal.timeout(4000) })
  apiReachable = true
  softCheck(
    `GET ${API}/api/v1/agent/hermes/readiness`,
    health.ok,
    `status ${health.status}`,
  )
} catch (err) {
  softCheck(
    `GET ${API}/api/v1/agent/hermes/readiness reachable`,
    false,
    `platform-api not running: ${err instanceof Error ? err.message : String(err)}`,
  )
}

try {
  const insights = await fetch(`${API}/api/v1/hermes/insights?limit=3`, {
    signal: AbortSignal.timeout(4000),
  })
  check(
    'GET /api/v1/hermes/insights?limit=3',
    insights.ok,
    `status ${insights.status}`,
  )
} catch (err) {
  check(
    'GET /api/v1/hermes/insights reachable',
    false,
    err instanceof Error ? err.message : String(err),
  )
}

const hard = checks.filter(c => !c.soft)
const soft = checks.filter(c => c.soft)
const hardPass = hard.filter(c => c.ok).length
const softPass = soft.filter(c => c.ok).length

console.log(`\nThree Desks: ${hardPass}/${hard.length} PASS · ${softPass}/${soft.length} soft\n`)
for (const c of checks) {
  const mark = c.ok ? '✓' : c.soft ? '○' : '✗'
  const tag = c.soft ? ' [soft]' : ''
  console.log(`${mark} ${c.name}${tag}${c.detail ? ` — ${c.detail}` : ''}`)
}

if (!apiReachable) {
  console.log(`\n(info) platform-api optional at ${API} — frontend assertions still gate exit.`)
}

process.exit(hard.every(c => c.ok) ? 0 : 1)
