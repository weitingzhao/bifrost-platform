/**
 * GET /market/doctor · POST /market/doctor/heal — check now, fix now.
 *
 * The doctor names what the last session should hold and what it does, one
 * finding per check, each with a prescription the plugin can execute. GET is
 * a bare fetch through the platform-api proxy like the other reads; heal is
 * an operator POST (platform-api swaps the bearer for the plugin write token).
 */
import { authedFetch } from './client'

export type DoctorSeverity = 'ok' | 'warn' | 'crit'
export type DoctorVerdict = 'healthy' | 'degraded' | 'critical'

export type DoctorFix = {
  action: 'enqueue-slot' | 'retry-jobs' | 'rollout-restart' | 'check-vendor-key' | string
  slot?: string
  date?: string
  force?: boolean
  kind?: string
  job_ids?: number[]
  deployment?: string
}

export type DoctorFinding = {
  id: string
  slot: string
  severity: DoctorSeverity
  title: string
  expected: unknown
  actual: unknown
  detail: string
  session?: string | null
  fix?: DoctorFix | null
  auto_fixable: boolean
  missing_sample?: string[]
}

export type DoctorPrescription = DoctorFix & { finding_ids: string[] }

export type DoctorReport = {
  ok: boolean
  generated_at: string
  session: string
  session_is_today: boolean
  universe: { watchlist: number; underlyings: number; optionable: number }
  verdict: DoctorVerdict
  summary: string
  findings: DoctorFinding[]
  prescriptions: DoctorPrescription[]
  retired_slots: string[]
}

export type HealAction = DoctorFix & {
  finding_ids: string[]
  result: 'dry_run' | string | Record<string, unknown>
}

export type HealResponse = {
  ok: boolean
  dry_run: boolean
  session: string | null
  verdict_before: DoctorVerdict | null
  actions: HealAction[]
  enqueued: number
}

const BASE = '/api/v1/plugins/market-data/api/market/doctor'

export async function fetchMarketDataDoctor(probes = true): Promise<DoctorReport> {
  const r = await fetch(probes ? BASE : `${BASE}?probes=false`)
  if (!r.ok) throw new Error(`market doctor: HTTP ${r.status}`)
  return (await r.json()) as DoctorReport
}

export async function healMarketData(body: {
  dry_run?: boolean
  finding_ids?: string[]
}): Promise<HealResponse> {
  const r = await authedFetch('market-data heal', `${BASE}/heal`, {
    method: 'POST',
    body: JSON.stringify(body),
  })
  return (await r.json()) as HealResponse
}
