/**
 * Control Room Phase 3 — mission-scoped command intents for the Commander diagnosis zone.
 */

import type { MatrixResponse, OpsContextResponse } from '@/api/types'
import {
  buildSessionPack,
  packForMode,
  suggestAgentMode,
  STARTER_PROMPTS,
  type AgentMode,
} from '@/lib/control-room/agentContextPacks'
import { buildControlRoomDispatchPack, PLATFORM_RELEASE_AGENT_PROMPT } from '@/lib/control-room/controlRoomOperatePack'
import { evaluatePromoteStatus } from '@/lib/control-room/matrixSummary'
import type { MissionSnapshot } from '@/lib/control-room/missionSignals'
import type { TrackId } from '@/lib/briefing/workTracks'

export type CommandIntentAction =
  | { type: 'agent_prefill'; prefill: string }
  | { type: 'copy_text'; text: string }
  | { type: 'open_briefing'; track?: string }
  | { type: 'open_delivery' }
  | { type: 'open_promote' }

export type CommandIntentChip = {
  id: string
  label: string
  detail: string
  emphasis?: 'primary' | 'default'
  action: CommandIntentAction
}

export type CommandIntentStripModel = {
  suggestedMode: AgentMode
  focusHeadline: string | null
  primaryChips: CommandIntentChip[]
  copyPacks: Array<{ mode: AgentMode | 'session'; label: string; text: string }>
}

function payloadVerifyPrefill(
  snapshot: MissionSnapshot,
  matrices: MatrixResponse[],
  context?: OpsContextResponse,
): string {
  const base = buildControlRoomDispatchPack({ snapshot, matrices, context })
  if (base != null) return base
  return [
    '# Control Room — payload verification',
    '',
    `Payload status: ${snapshot.payloadOverall}.`,
    '',
    'Read-only verification: list failing matrix targets for dev and prod, hypothesize root cause (network, K3s, datastore), propose probes only — no writes without approval.',
  ].join('\n')
}

function promoteAssessPrefill(context: OpsContextResponse, matrices: MatrixResponse[]): string {
  return packForMode('Promote', context, matrices)
}

function agentSessionPrefill(
  context: OpsContextResponse | undefined,
  matrices: MatrixResponse[],
  mode: AgentMode,
): string {
  if (context == null) return STARTER_PROMPTS[mode]
  const pack = buildSessionPack(context, matrices)
  return `${pack}\n\n---\n\n${STARTER_PROMPTS[mode]}`
}

export function buildCommandIntentStripModel(input: {
  snapshot: MissionSnapshot
  matrices: MatrixResponse[]
  context?: OpsContextResponse
}): CommandIntentStripModel {
  const { snapshot, matrices, context } = input
  const suggestedMode = suggestAgentMode(context)
  const primaryChips: CommandIntentChip[] = []

  if (snapshot.payloadOverall !== 'ok') {
    primaryChips.push({
      id: 'verify-payload',
      label: 'Verify payload',
      detail: 'Read-only matrix + datastore diagnosis',
      emphasis: 'primary',
      action: {
        type: 'agent_prefill',
        prefill: payloadVerifyPrefill(snapshot, matrices, context),
      },
    })
  }

  if (snapshot.release.signal !== 'ok') {
    primaryChips.push({
      id: 'release-review',
      label: 'Review release',
      detail: 'Open Delivery — STG deliver & gates',
      action: { type: 'open_delivery' },
    })
    primaryChips.push({
      id: 'release-agent',
      label: 'Platform release (Agent)',
      detail: 'Dispatch release-scoped Agent task',
      action: {
        type: 'agent_prefill',
        prefill: PLATFORM_RELEASE_AGENT_PROMPT,
      },
    })
  }

  if (context != null) {
    const promote = evaluatePromoteStatus(context, matrices)
    if (!promote.ready) {
      primaryChips.push({
        id: 'assess-promote',
        label: 'Assess promote',
        detail: promote.reasons[0] ?? 'Coupling gate blocked',
        action: {
          type: 'agent_prefill',
          prefill: promoteAssessPrefill(context, matrices),
        },
      })
    }
  }

  if (snapshot.missionOverall === 'ok') {
    primaryChips.push({
      id: 'routine-ops',
      label: 'Routine Ops check',
      detail: 'Spine + matrix read-only sweep',
      action: {
        type: 'agent_prefill',
        prefill: context != null ? agentSessionPrefill(context, matrices, 'Ops') : STARTER_PROMPTS.Ops,
      },
    })
  }

  primaryChips.push({
    id: 'agent-session',
    label: 'Agent session pack',
    detail: `Suggested mode: ${suggestedMode}`,
    emphasis: primaryChips.length === 0 ? 'primary' : 'default',
    action: {
      type: 'agent_prefill',
      prefill: agentSessionPrefill(context, matrices, suggestedMode),
    },
  })

  const briefingTrack: TrackId =
    context?.focus.flywheel_primary === 'B' || snapshot.payloadOverall !== 'ok' ? 'operate' : 'build'

  primaryChips.push({
    id: 'open-briefing',
    label: 'Agent Briefing',
    detail: `Track: ${briefingTrack}`,
    action: { type: 'open_briefing', track: briefingTrack },
  })

  const copyPacks: CommandIntentStripModel['copyPacks'] = []
  if (context != null) {
    copyPacks.push({ mode: 'session', label: 'Session', text: buildSessionPack(context, matrices) })
    copyPacks.push({ mode: 'Ops', label: 'Ops', text: packForMode('Ops', context, matrices) })
    copyPacks.push({ mode: 'Promote', label: 'Promote', text: packForMode('Promote', context, matrices) })
  } else {
    copyPacks.push({ mode: 'Product', label: 'Product', text: packForMode('Product', undefined, matrices) })
  }

  const deduped = primaryChips.filter(
    (chip, idx, arr) => arr.findIndex(c => c.id === chip.id) === idx,
  )

  return {
    suggestedMode,
    focusHeadline: context?.focus.headline ?? null,
    primaryChips: deduped.slice(0, 6),
    copyPacks,
  }
}
