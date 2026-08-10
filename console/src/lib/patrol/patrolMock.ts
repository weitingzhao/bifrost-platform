import type { PatrolRun, PatrolSkill } from '@/api/patrol'

function hoursAgo(hours: number, now = Date.now()): string {
  return new Date(now - hours * 3_600_000).toISOString()
}

function hoursFromNow(hours: number, now = Date.now()): string {
  return new Date(now + hours * 3_600_000).toISOString()
}

const SEED_NOW = Date.now()

/**
 * DEV-only storybook fallback. Live UI must not import this unless
 * `import.meta.env.DEV && import.meta.env.VITE_PATROL_MOCK === '1'`.
 * Default path is empty/Idle — never fake-green production.
 */
export const PATROL_MOCK_SKILLS: PatrolSkill[] = [
  {
    id: 'fleet-drift-scan',
    name: 'Fleet drift scan',
    description: 'Compare live fleet probes against expected overlays (read-only).',
    schedule: '0 3 * * *',
    prompt_template: '',
    mcp_tools: ['get_cluster_summary', 'get_cluster_nodes'],
    trust_level: 'L0',
    scope: 'cluster',
    timeout_seconds: 120,
    enabled: true,
    last_run_at: hoursAgo(5, SEED_NOW),
    last_result: 'success',
    next_run_at: hoursFromNow(7, SEED_NOW),
  },
  {
    id: 'cert-expiry-check',
    name: 'Cert expiry check',
    description: 'Scan TLS certs for expiry within 30 days.',
    schedule: '0 6 * * 1',
    prompt_template: '',
    mcp_tools: ['get_cluster_summary'],
    trust_level: 'L0',
    scope: 'platform',
    timeout_seconds: 90,
    enabled: true,
    last_run_at: hoursAgo(28, SEED_NOW),
    last_result: 'success',
    next_run_at: hoursFromNow(140, SEED_NOW),
  },
  {
    id: 'stale-pod-cleanup',
    name: 'Stale pod cleanup',
    description: 'Evict Completed/Failed pods older than retention (L1 actuation).',
    schedule: '0 4 * * *',
    prompt_template: '',
    mcp_tools: ['get_cluster_nodes', 'delete_pod'],
    trust_level: 'L1',
    scope: 'cluster',
    timeout_seconds: 180,
    enabled: true,
    last_run_at: hoursAgo(14, SEED_NOW),
    last_result: 'success',
    next_run_at: hoursFromNow(10, SEED_NOW),
  },
]

export const PATROL_MOCK_RUNS: PatrolRun[] = [
  {
    id: 'run-fleet-drift-001',
    skill_id: 'fleet-drift-scan',
    skill_name: 'Fleet drift scan',
    trigger: 'cron',
    started_at: hoursAgo(5.05, SEED_NOW),
    finished_at: hoursAgo(5, SEED_NOW),
    duration_ms: 42_100,
    result: 'success',
    evidence: 'No overlay drift vs expected replica counts.',
  },
  {
    id: 'run-cert-001',
    skill_id: 'cert-expiry-check',
    skill_name: 'Cert expiry check',
    trigger: 'cron',
    started_at: hoursAgo(28.02, SEED_NOW),
    finished_at: hoursAgo(28, SEED_NOW),
    duration_ms: 18_400,
    result: 'success',
    evidence: 'All scanned certs > 30d remaining.',
  },
  {
    id: 'run-stale-pod-001',
    skill_id: 'stale-pod-cleanup',
    skill_name: 'Stale pod cleanup',
    trigger: 'cron',
    started_at: hoursAgo(14.01, SEED_NOW),
    finished_at: hoursAgo(14, SEED_NOW),
    duration_ms: 6_200,
    result: 'success',
    evidence: 'No Completed/Failed pods past retention.',
  },
]
