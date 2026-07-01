/**
 * Control Room Operate Loop — dispatch packs for Agent Desk / remediation runner.
 */

import type { MatrixResponse, OpsContextResponse, VerifyMissionSnapshotResponse, VerifyPayloadResponse } from '@/api/types'
import { buildOpsPack } from '@/lib/control-room/agentContextPacks'
import {
  buildDiagnosticPrompt,
  missionStatus,
  type MissionSnapshot,
} from '@/lib/control-room/missionSignals'

/** Same prompt as Agent Desk → Platform release quick action. */
export const PLATFORM_RELEASE_AGENT_PROMPT =
  'Deploy latest changes to prod. Scan all repos for uncommitted changes, commit and push, then run the full STG → Prod pipeline.'

export type FailingMatrixTarget = {
  environment: string
  id: string
  reachability: string
  detail?: string
}

export function listFailingMatrixTargets(matrices: MatrixResponse[]): FailingMatrixTarget[] {
  const out: FailingMatrixTarget[] = []
  for (const m of matrices) {
    for (const t of m.targets) {
      if (t.reachability === 'fail' || t.reachability === 'degraded') {
        out.push({
          environment: m.environment,
          id: t.id,
          reachability: t.reachability,
          detail: t.detail,
        })
      }
    }
  }
  return out
}

const COMPACT_OPS_MAX_CHARS = 2_400

function truncatePack(text: string, max: number): string {
  if (text.length <= max) return text
  return `${text.slice(0, max)}\n\n… (compact ops context truncated — open Briefing for full pack)`
}

/**
 * Rich dispatch pack: mission diagnosis + failing matrix targets + compact ops spine.
 * Used by Control Room Diagnose & Fix → Agent Desk prefill.
 */
export function buildControlRoomDispatchPack(input: {
  snapshot: MissionSnapshot
  matrices: MatrixResponse[]
  context?: OpsContextResponse
  verify?: VerifyPayloadResponse
}): string | null {
  const base = buildDiagnosticPrompt(input.snapshot, input.verify)
  if (base == null) return null

  const lines: string[] = [
    '# Control Room — dispatch brief',
    '',
    base,
  ]

  const failing = listFailingMatrixTargets(input.matrices)
  if (failing.length > 0) {
    lines.push('', '## Failing matrix targets')
    for (const t of failing.slice(0, 24)) {
      const detail = t.detail != null && t.detail !== '' ? ` — ${t.detail}` : ''
      lines.push(`- ${t.environment} · ${t.id} (${t.reachability})${detail}`)
    }
    if (failing.length > 24) {
      lines.push(`- … and ${failing.length - 24} more (open Runtime Map)`)
    }
  }

  if (input.context != null) {
    const compactOps = truncatePack(buildOpsPack(input.context, input.matrices, { compact: true }), COMPACT_OPS_MAX_CHARS)
    lines.push('', '## Compact ops context', '', compactOps)
  }

  lines.push(
    '',
    '## Operator intent',
    'Diagnose root causes for the issues above. For read-only checks, proceed automatically. For destructive actions or prod writes, request operator approval first.',
  )

  return lines.join('\n')
}

export function buildMissionVerifyMessage(
  snapshot: MissionSnapshot,
  jobStatus: 'done' | 'failed',
  verifySnapshot?: VerifyMissionSnapshotResponse,
): { nominal: boolean; headline: string; detail: string } {
  const mission = missionStatus(snapshot.missionOverall)
  const postFix = verifySnapshot?.post_fix_verification
  const payloadClass = verifySnapshot?.payload_verification.summary.overall

  if (jobStatus === 'failed') {
    return {
      nominal: false,
      headline: 'Agent run failed',
      detail: `Mission remains ${mission}. Review the job log in Agent Desk.`,
    }
  }

  const cockpitNominal = snapshot.missionOverall === 'ok'
  const postFixPassed = postFix?.passed === true

  if (cockpitNominal && postFixPassed) {
    return {
      nominal: true,
      headline: 'Verified NOMINAL',
      detail: `Post-fix reprobe passed — cockpit ${mission}, verify_payload ${payloadClass ?? 'NOMINAL'}.`,
    }
  }

  if (postFix != null && !postFixPassed) {
    return {
      nominal: false,
      headline: 'Agent done — post-fix NOT passed',
      detail: postFix.detail,
    }
  }

  if (cockpitNominal) {
    return {
      nominal: true,
      headline: 'Verified NOMINAL',
      detail: 'Latest Agent run finished — mission probes report NOMINAL.',
    }
  }

  return {
    nominal: false,
    headline: `Agent done — Mission ${mission}`,
    detail:
      postFix?.agent_guidance ??
      'Agent finished but probes still report issues. Review remaining failures or dispatch again.',
  }
}
