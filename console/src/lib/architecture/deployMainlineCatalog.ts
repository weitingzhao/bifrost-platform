/**
 * Deploy Mainline catalog — deployment decision chain & sign-off gates.
 *
 * Authoritative source for Ops Console → Program → Deploy Mainline.
 * Migrated from bifrost-trade-infra/docs/LOCAL_PROD_FINAL_SIGNOFF.md (2026-06-15).
 */

export const DEPLOY_MAINLINE_VERSION = '2026-06-15'
export const DEPLOY_MAINLINE_SOURCE = 'console/src/lib/architecture/deployMainlineCatalog.ts'
export const DEPLOY_MAINLINE_STATUS =
  'Local Prod Final CLOSED (2026-06-04). K3s Phase 1 in progress. Prod cutover blocked on D1.'

export type MainlinePhase = {
  seq: number
  phase: string
  authority: string
  status: string
}

export const MAINLINE_PHASES: MainlinePhase[] = [
  { seq: 0, phase: 'Phase 2B + 2C-A Session 0–9', authority: 'PHASE2C_SIGNOFF_MASTER.md', status: 'CLOSED (2026-06-08)' },
  { seq: 1, phase: 'Local Prod Final', authority: 'This page', status: 'CLOSED (2026-06-04 Owner L4)' },
  { seq: 2, phase: '2C-B Linux Docker Prod (stability test)', authority: 'PHASE2C_SIGNOFF_MASTER.md §2C-B', status: 'Stability tested (D5); prod cutover pending migration plan' },
  { seq: 3, phase: 'K3s Phase 1 trial', authority: 'Ops Console → Architecture → K3s Architecture §10', status: 'In progress (Owner unlocked 2026-06-04; bootstrap CLOSED 2026-06-14)' },
  { seq: 4, phase: 'Compose → K3s migration', authority: 'Ops Console → Architecture → Platform Roadmap §5–6', status: 'Pending K3s cluster ready + D1 decision' },
  { seq: 5, phase: 'Phase 3 Legacy retirement', authority: 'PHASE2C_PROD_DEFERRED.md', status: 'Pending Prod full-stack validation' },
]

export const PHASE_L_CONTEXT = {
  relation: '2C-A (2026-06-08): Sessions 0–9 all Owner signed, CLOSED.',
  purpose: 'Final mechanical revalidation + Owner short-list confirmation under local http://localhost/ stack, proving it can serve as reference baseline for subsequent K3s / production migration.',
  notEquals: 'Not equivalent to production cutover.',
}

export type L1Check = {
  check: string
  pass: boolean
  agentDate: string
  remarks: string
}

export const L1_CHECKS: L1Check[] = [
  { check: 'make prod-health (PG + Redis + nginx + 9 API)', pass: true, agentDate: '2026-06-08', remarks: 'postgres .80, redis .70, 12/12 OK' },
  { check: 'make verify-2c-a1', pass: true, agentDate: '2026-06-08', remarks: 'docker executor; destructive SKIP acceptable' },
  { check: 'SPA http://localhost/ HTTP 200', pass: true, agentDate: '2026-06-08', remarks: 'local_prod_final_gate.sh' },
  { check: 'bifrost-platform API /health (optional)', pass: true, agentDate: '2026-06-08', remarks: ':8780' },
  { check: 'GET /api/v1/topology?env=prod (optional)', pass: true, agentDate: '2026-06-08', remarks: 'Topology API OK' },
]

export type L2Session = {
  session: string
  item: string
  route: string
  pass: boolean
  ownerDate: string
  remarks: string
}

export const L2_SESSIONS: L2Session[] = [
  { session: '0', item: 'L2.7', route: '/settings/api + /', pass: true, ownerDate: '2026-06-04', remarks: 'Dev/Prod dual-column all-red non-blocking; Swagger Open blank known' },
  { session: '1', item: 'L2.1', route: '/ Global strip, sidebar lamp', pass: true, ownerDate: '2026-06-04', remarks: '' },
  { session: '1', item: 'L2.2', route: '/operations/daemon overview', pass: true, ownerDate: '2026-06-04', remarks: '' },
  { session: '2', item: 'L2.5', route: '/market/live SSE', pass: true, ownerDate: '2026-06-04', remarks: '' },
  { session: '3', item: 'L2.6', route: '/portfolio/positions', pass: true, ownerDate: '2026-06-04', remarks: '' },
  { session: '8', item: 'L2.2–L2.4', route: 'daemon + celery + socket', pass: true, ownerDate: '2026-06-04', remarks: 'config.prod no token; anonymous operator start/stop OK' },
  { session: 'opt', item: 'L2.8', route: ':5180 Topology/Matrix', pass: true, ownerDate: '2026-06-04', remarks: 'Platform Console' },
]

export const L2_KNOWN_NON_BLOCKERS = [
  '/settings/api Dev/Prod port dual-column probes all-red',
  'Socket single slot yellow lamp',
  'Swagger/ReDoc Open vs nginx prefix misalignment',
]

export type D_Decision = {
  id: string
  draft: string
  ownerDecision: string
  ownerDate: string
}

export const L3_DECISIONS: D_Decision[] = [
  {
    id: 'D1',
    draft: '2C-B Prod host = mini-pc-a (.70)',
    ownerDecision: 'Build K3s cluster first; Compose→K3s migration path TBD after cluster ready (do not pre-lock .70 as sole Prod target)',
    ownerDate: '2026-06-04',
  },
  {
    id: 'D2',
    draft: 'PG stays on mini-pc-b (.80)',
    ownerDecision: 'Confirmed: .80 bare-metal PG stays unchanged (until CNPG migration)',
    ownerDate: '2026-06-04',
  },
  {
    id: 'D3',
    draft: 'TWS = Win11 Host',
    ownerDecision: 'Confirmed: TWS Host + Secondary both on Win11; IB_HOST configured per account',
    ownerDate: '2026-06-04',
  },
  {
    id: 'D4',
    draft: 'R-DV3: only New daemon auto-trades',
    ownerDecision: 'Deferred: no auto-trade requirement currently (too risky); no maintenance window auto-trade switching',
    ownerDate: '2026-06-04',
  },
  {
    id: 'D5',
    draft: '2C-B before K3s',
    ownerDecision: 'Revised: 2C-B stability test signed; can immediately start K3s env build & test (parallel with Compose stable state)',
    ownerDate: '2026-06-04',
  },
]

export type L4Signoff = {
  item: string
  pass: boolean
  ownerDate: string
  signee: string
}

export const L4_SIGNOFF: L4Signoff[] = [
  { item: 'L1 mechanical gate all-green (or SKIP-acceptable only)', pass: true, ownerDate: '2026-06-04', signee: 'Agent 2026-06-08' },
  { item: 'L2 browser short-list L2.1–L2.8', pass: true, ownerDate: '2026-06-04', signee: 'Owner' },
  { item: 'L3 decisions D1–D5 confirmed', pass: true, ownerDate: '2026-06-04', signee: 'Owner' },
  { item: 'Local Prod Final CLOSED', pass: true, ownerDate: '2026-06-04', signee: 'Owner' },
]

export const POST_SIGNOFF_UNLOCK =
  'K3s Phase 1 (Ops Console → Architecture → K3s Architecture §10); 2C-B production runbook retained as Compose reference; prod cutover pending D1 migration decision.'

export type NextPhaseItem = { label: string; detail: string }

export const NEXT_K3S_STEPS: NextPhaseItem[] = [
  { label: 'mini-pc-a', detail: 'Ubuntu 24.04 + K3s Server (single-node validation)' },
  { label: 'mini-pc-b', detail: 'Server join / verify CNPG Operator' },
  { label: 'gpu-server (4090)', detail: 'K3s Agent + workload=gpu' },
  { label: 'Mac Mini x2', detail: 'UTM Ubuntu Agent — P5b CLOSED (ops-vm-ubt-01/.54, ops-vm-ubt-02/.56)' },
  { label: 'Console', detail: 'Update deployment_phase: k3s then verify topology' },
]

export const COMPOSE_REFERENCE_COMMANDS = [
  'cd bifrost-trade-infra',
  'make sync-prod-config',
  'make prod-preflight',
  'make prod-health',
]

export const MIGRATION_SEQUENCE = [
  'data → socket/worker → api → frontend (Platform Roadmap)',
  'Legacy retirement: Phase 3, requires K3s or Compose Prod full-stack validation',
  'Auto-trade / R-DV3: Owner deferred, not in current milestone scope',
]

export type ChangeLogEntry = { date: string; content: string }

export const CHANGE_LOG: ChangeLogEntry[] = [
  { date: '2026-06-08', content: 'Created; Agent prod-health + verify-2c-a1 revalidation passed' },
  { date: '2026-06-04', content: 'Owner L2 Sessions 0–3/8 + L2.8; L3 D1–D5 revised; L4 CLOSED; K3s Phase 1 unlocked' },
]

export function buildDeployMainlineLlmPack(): string {
  const lines: string[] = [
    '# Bifrost Ops — Deploy Mainline (Decision Chain & Sign-off Gates)',
    `# Source: ${DEPLOY_MAINLINE_SOURCE} v${DEPLOY_MAINLINE_VERSION}`,
    `Status: ${DEPLOY_MAINLINE_STATUS}`,
    '',
    '## Mainline phases',
    ...MAINLINE_PHASES.map(p => `${p.seq}. **${p.phase}** — ${p.status} (authority: ${p.authority})`),
    '',
    '## Phase L — Local Prod Final (2C-B pre-gate)',
    `Relation: ${PHASE_L_CONTEXT.relation}`,
    `Purpose: ${PHASE_L_CONTEXT.purpose}`,
    PHASE_L_CONTEXT.notEquals,
    '',
    '## L1 — Agent mechanical gate',
    ...L1_CHECKS.map(c => `- [${c.pass ? 'x' : ' '}] ${c.check} — ${c.remarks}`),
    '',
    '## L2 — Owner browser short-list',
    ...L2_SESSIONS.map(s => `- [x] Session ${s.session} ${s.item}: ${s.route} — ${s.remarks || 'OK'}`),
    'Known non-blockers:',
    ...L2_KNOWN_NON_BLOCKERS.map(n => `  - ${n}`),
    '',
    '## L3 — Owner decisions (2026-06-04)',
    ...L3_DECISIONS.map(d => `- **${d.id}**: Draft: ${d.draft} → Owner: ${d.ownerDecision}`),
    '',
    '## L4 — Local Prod Final sign-off',
    ...L4_SIGNOFF.map(s => `- [${s.pass ? 'x' : ' '}] ${s.item} — ${s.signee} ${s.ownerDate}`),
    `Post-signoff: ${POST_SIGNOFF_UNLOCK}`,
    '',
    '## Next: K3s Phase 1',
    ...NEXT_K3S_STEPS.map(s => `- ${s.label}: ${s.detail}`),
    '',
    '## Migration sequence',
    ...MIGRATION_SEQUENCE.map(m => `- ${m}`),
  ]
  return lines.join('\n')
}
