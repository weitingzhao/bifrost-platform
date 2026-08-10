#!/usr/bin/env node
/** Wave 4b — Mission Signal program static verification. */
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

const yaml = readRepo('config/programs/completed/mission-signal.yaml')
const catalog = read('src/lib/architecture/missionSignalCatalog.ts')

check('mission-signal.yaml has P1–P7', ['P1', 'P2', 'P3', 'P4', 'P5', 'P6', 'P7'].every(id => yaml.includes(`id: ${id}`)))
check('mission-signal sign_off_mechanism api', yaml.includes('sign_off_mechanism: api'))
check('P1 acceptance checklist', yaml.includes('Mission Control header'))
check('P7 depends on P1–P6', yaml.includes('depends_on: [P1, P2, P3, P4, P5, P6]'))
check('missionSignalCatalog.ts', fs.existsSync(path.join(root, 'src/lib/architecture/missionSignalCatalog.ts')))
check('catalog MISSION_SIGNAL_PHASES length 7', catalog.includes('MISSION_SIGNAL_PHASES') && (catalog.match(/id: 'P/g) ?? []).length >= 7)
check('useMissionSignalPhaseReadiness hook', fs.existsSync(path.join(root, 'src/hooks/useMissionSignalPhaseReadiness.ts')))
check('MissionSignalProgramPanels', read('src/components/delivery/DeliveryBoardProgramPanels.tsx').includes('MissionSignalProgramPanels'))
check('mission-signal panel mount', read('src/components/delivery/DeliveryBoardProgramPanels.tsx').includes("programId === 'mission-signal'"))
check('MissionSignalPhasePanel signoff API', read('src/components/delivery/MissionSignalPhasePanel.tsx').includes('signoffProgramPhase'))
check('ProgramDetailView mission-signal panel sign-off', read('src/components/delivery/ProgramDetailView.tsx').includes('isMissionSignalProgram'))
check('Control Room MissionSignalProgramStrip', read('src/pages/ControlRoomPage.tsx').includes('MissionSignalProgramStrip'))
check('readiness uses verify-payload', read('src/hooks/useMissionSignalPhaseReadiness.ts').includes('fetchVerifyPayload'))
check('readiness uses verify-snapshot', read('src/hooks/useMissionSignalPhaseReadiness.ts').includes('fetchVerifyMissionSnapshot'))
check('readiness uses hermes', read('src/hooks/useMissionSignalPhaseReadiness.ts').includes('fetchHermesReadiness'))
check('readiness uses flight director', read('src/hooks/useMissionSignalPhaseReadiness.ts').includes('fetchFlightDirectorSnapshot'))
check('mission verify API routes', readRepo('api/internal/server/server.go').includes('/mission/verify-payload'))
check('hermes readiness route', readRepo('api/internal/server/server.go').includes('/agent/hermes/readiness'))
check('governance snapshot route', readRepo('api/internal/server/server.go').includes('/agent/governance/snapshot'))

const pass = checks.filter(c => c.ok).length
const total = checks.length
console.log(`\nWave 4b Mission Signal: ${pass}/${total} PASS\n`)
for (const c of checks) {
  console.log(`${c.ok ? '✓' : '✗'} ${c.name}${c.detail ? ` — ${c.detail}` : ''}`)
}
process.exit(pass === total ? 0 : 1)
