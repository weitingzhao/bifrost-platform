import { appendEvent, makeEvent } from './jobs.js'
import { jsonText, platformGet } from './platformClient.js'

export type PostFixVerificationResult = {
  passed: boolean
  mission_matrix_nominal: boolean
  datastore_verification_nominal: boolean
  probe_drift_remaining: boolean
  detail: string
  agent_guidance: string
}

export type VerifyMissionSnapshotResponse = {
  generated_at: string
  trade_dev: { environment: string; signal: string; reachable: number; total: number; detail: string }
  trade_prod: { environment: string; signal: string; reachable: number; total: number; detail: string }
  payload_overall: string
  payload_verification: unknown
  post_fix_verification: PostFixVerificationResult
}

export async function runPostFixVerification(jobId: string): Promise<PostFixVerificationResult | null> {
  try {
    const data = (await platformGet('/api/v1/mission/verify-snapshot')) as VerifyMissionSnapshotResponse
    const pf = data.post_fix_verification
    const label = pf.passed ? 'PASSED' : 'NOT PASSED'
    appendEvent(
      jobId,
      makeEvent('status', `post_fix_verification: ${label} — ${pf.detail}`, {
        kind: 'post_fix_verification',
        passed: pf.passed,
        detail: pf.detail,
        agent_guidance: pf.agent_guidance,
      }),
    )
    return pf
  } catch (err) {
    const message = err instanceof Error ? err.message : String(err)
    appendEvent(jobId, makeEvent('error', `post_fix_verification probe failed: ${message}`, {
      kind: 'post_fix_verification',
      passed: false,
    }))
    return null
  }
}

export function formatPostFixSummary(pf: PostFixVerificationResult | null, agentSummary: string): string {
  if (pf == null) {
    return `${agentSummary}\n\nPost-fix verification: probe failed — re-run verify_mission_snapshot manually.`
  }
  const suffix = pf.passed
    ? 'Post-fix verification: PASSED.'
    : `Post-fix verification: NOT PASSED — ${pf.detail}. ${pf.agent_guidance}`
  const base = agentSummary.trim() === '' ? '' : `${agentSummary.trim()}\n\n`
  return `${base}${suffix}`
}

export function postFixJsonForTool(data: unknown): string {
  return jsonText(data)
}
