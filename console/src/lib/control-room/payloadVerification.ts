/**
 * Payload verification — matrix vs cluster classification for Agent playbooks.
 * Mirrors GET /api/v1/mission/verify-payload (probe.VerifyPayload).
 */

import type { VerifyPayloadResponse } from '@/api/types'

export type PayloadClassification = 'NOMINAL' | 'PROBE_DRIFT' | 'DATA_LAYER' | 'HTTP_FAIL' | 'UNKNOWN'

export function formatVerifyPayloadGuidance(verify: VerifyPayloadResponse | undefined): string[] {
  if (verify == null || verify.environments.length === 0) return []

  const lines: string[] = [
    '',
    '## Payload verification (verify_payload)',
    '',
    'Before remediating datastore or trade HTTP targets, classify using matrix vs cluster API:',
    '',
    '| Class | Meaning | Agent action |',
    '|-------|---------|--------------|',
    '| NOMINAL | Matrix and cluster agree — healthy | No datastore fix needed |',
    '| PROBE_DRIFT | Matrix fail but cluster ok (e.g. in-cluster DNS from Mac) | Platform probe defect — L2 Owner / fix probe path, do NOT restart PG/Redis |',
    '| DATA_LAYER | Matrix and/or cluster report datastore down | L1 confirm — investigate CNPG/Redis, may restart or escalate |',
    '| HTTP_FAIL | Trade API/frontend HTTP probe fail | Diagnose nginx/API pods — not a datastore issue |',
    '',
    `Summary: overall=${verify.summary.overall} · nominal=${verify.summary.nominal_count} · probe_drift=${verify.summary.probe_drift_count} · data_layer=${verify.summary.data_layer_count} · http_fail=${verify.summary.http_fail_count}`,
    '',
    'Per environment:',
  ]

  for (const env of verify.environments) {
    lines.push(
      `- **${env.environment}** (${env.classification}): PG ${env.postgres.classification} · Redis ${env.redis.classification}${env.http_failures.length > 0 ? ` · HTTP fail: ${env.http_failures.join(', ')}` : ''}`,
    )
    if (env.classification === 'PROBE_DRIFT') {
      lines.push(`  - ${env.detail} — do not treat as payload outage`)
    }
  }

  lines.push('', 'MCP: `verify_payload` · API: GET /api/v1/mission/verify-payload')
  return lines
}

export function hasProbeDrift(verify: VerifyPayloadResponse | undefined): boolean {
  return (verify?.summary.probe_drift_count ?? 0) > 0
}

export function hasDataLayerFail(verify: VerifyPayloadResponse | undefined): boolean {
  return (verify?.summary.data_layer_count ?? 0) > 0
}
