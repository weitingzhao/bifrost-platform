/**
 * Launch Agent ambient agent — L-1 Mac Mini host publish (not Tekton / not in-cluster).
 */

import type { AgentBridgeResponse, AgentDeployStatusResponse } from '@/api/agentTypes'
import type {
  AgentLaunchEvidence,
  AgentLaunchTargetId,
} from '@/lib/delivery/agentLaunchEvidence'

export const AGENT_LAUNCH_SCOPE = 'agent-launch'

export interface AgentLaunchPromptContext {
  target: AgentLaunchTargetId
  bridge?: AgentBridgeResponse
  deployStatus?: AgentDeployStatusResponse
  evidence?: AgentLaunchEvidence
  outcomeKind?: 'released' | 'in_progress' | 'failed' | 'idle'
  outcomeDetail?: string
  operatorSurface?: string
}

export function buildAgentLaunchPrompt(ctx: AgentLaunchPromptContext): string {
  const surface = ctx.operatorSurface ?? 'Launch Desk · Launch Agent'
  const runners =
    ctx.bridge?.runners?.map(r => ({
      role: r.role,
      status: r.status,
      active: r.active,
      url: r.url,
    })) ?? []
  const snapshot = {
    target: ctx.target,
    deploy: {
      enabled: ctx.deployStatus?.enabled ?? null,
      targets: ctx.deployStatus?.targets ?? null,
      current: ctx.deployStatus?.current ?? null,
      last: ctx.deployStatus?.last ?? null,
      hint: ctx.deployStatus?.hint ?? null,
    },
    runners,
    evidence: ctx.evidence ?? null,
    console_view: {
      outcome: ctx.outcomeKind ?? 'unknown',
      detail: ctx.outcomeDetail ?? '',
    },
  }

  return [
    'Publish the Bifrost L-1 Agent host (remediation-runner) via Launch Desk → Agent.',
    'This is NOT a Tekton deliver-platform / deliver-stg pipeline and MUST NOT schedule into Kubernetes.',
    'Executor (after Owner approval): POST /api/v1/agent/deploy → deploy_mac_mini.sh (rsync + launchctl).',
    'Tools: get_agent_bridge, get_agent_deploy_status, start_agent_host_deploy (after request_operator_approval).',
    'D10: no place_order, no daemon scale-up.',
    '',
    `## Operator context (${surface})`,
    `The operator clicked **AI Launch Agent** with target **${ctx.target}**.`,
    '',
    '```json',
    JSON.stringify(snapshot, null, 2),
    '```',
    '',
    '## Workflow (strict)',
    '1. Detect — get_agent_bridge + get_agent_deploy_status. Report enabled, targets, runner heartbeats.',
    `2. Approve — request_operator_approval: "Publish Agent host (deploy_mac_mini.sh) to ${ctx.target}?"`,
    `3. Deploy — on approval: start_agent_host_deploy(target="${ctx.target}").`,
    '4. Verify — poll get_agent_deploy_status until done/failed; recheck get_agent_bridge.',
    '5. Live check — target runner heartbeat ok; report Detect→Approve→Deploy→Verify→Live evidence.',
    '',
    '## Must-not',
    '- Do not start bifrost-deliver-platform / bifrost-deliver-stg for Agent host publish.',
    '- Do not schedule remediation-runner into Kubernetes (L-1 fate isolation).',
    '- Do not enable live trading / scale daemon (D10 BLOCKED).',
    '',
    'Begin with Detect, then request approval before Deploy.',
  ].join('\n')
}
