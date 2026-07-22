import type { DeliveryPipelineRunView, ReleaseGateResponse, ReleaseStateResponse } from '@/api/deliveryTypes'
import { PLATFORM_RELEASE_AGENT_PROMPT } from '@/lib/control-room/controlRoomOperatePack'

export const PLATFORM_RELEASE_SCOPE = 'release'

export interface PlatformReleasePromptContext {
  releaseState?: ReleaseStateResponse
  stgRun?: DeliveryPipelineRunView
  prodRun?: DeliveryPipelineRunView
  stgGate?: ReleaseGateResponse
  prodGate?: ReleaseGateResponse
  outcomeKind?: 'released' | 'in_progress' | 'failed' | 'idle'
  outcomeDetail?: string
  activeRevision?: string | null
}

export function buildPlatformReleasePrompt(ctx: PlatformReleasePromptContext): string {
  const snapshot = {
    release_state: ctx.releaseState ?? null,
    stg_latest_run: summarizeRun(ctx.stgRun),
    prod_latest_run: summarizeRun(ctx.prodRun),
    stg_gate: summarizeGate(ctx.stgGate),
    prod_gate: summarizeGate(ctx.prodGate),
    console_view: {
      outcome: ctx.outcomeKind ?? 'unknown',
      detail: ctx.outcomeDetail ?? '',
      active_revision: ctx.activeRevision ?? null,
    },
  }

  const nextAction = ctx.releaseState?.next_action
  const nextHint =
    nextAction != null
      ? `Platform-api next_action: ${nextAction.label} — ${nextAction.description}`
      : 'Call get_release_state to determine the next phase.'

  return [
    PLATFORM_RELEASE_AGENT_PROMPT,
    '',
    '## Operator context (Launch Rocket page at task start)',
    'The operator clicked **AI Release** on the Launch Rocket page. Use the snapshot below plus live MCP tools.',
    '',
    '```json',
    JSON.stringify(snapshot, null, 2),
    '```',
    '',
    '## Guidance from current UI state',
    nextHint,
    ctx.outcomeKind === 'released'
      ? 'All four stages already show complete — confirm with get_release_state; only redeploy if operator wants a new revision.'
      : ctx.outcomeKind === 'failed'
        ? 'A stage failed — diagnose the failed phase first; use spawn_release_fix only for code/config failures per Release Agent rules.'
        : 'Execute the full STG → PROD release flow from Phase A unless operator chose skip-commit.',
    '',
    '## Reminders',
    '- Git Bridge runs on Mac Pro (not K8s). Verify git_workspace_status before commit.',
    '- **bifrost-ui is COPY-baked into platform-console image** — Tekton clones bifrost-platform + bifrost-ui from Gitea **main**. UI on a feature branch will NOT reach PROD until merged to main and redeployed.',
    '- If git_workspace_status shows bifrost-ui `needs_main_for_deploy`, stop and merge to main before Phase B.',
    '- bifrost-platform-plugin changes need Phase G (install-ib-gateway) after main pipeline.',
    '- Request operator approval before each gate and before PROD deploy.',
    '- Phase F: curl PROD console CSS and verify `scrollbar-thumb` / `--sidebar-border` tokens exist.',
  ].join('\n')
}

function summarizeRun(run: DeliveryPipelineRunView | undefined) {
  if (run == null) return null
  return {
    name: run.name,
    status: run.status,
    revision: run.revision,
    start_time: run.start_time,
    completion_time: run.completion_time,
  }
}

function summarizeGate(gate: ReleaseGateResponse | undefined) {
  if (gate == null) return null
  return {
    result: gate.result,
    revision: gate.revision,
    blockers: gate.blockers,
    check_count: gate.checks?.length ?? 0,
    failed_checks: gate.checks?.filter(c => c.reachability === 'fail').map(c => c.label) ?? [],
  }
}
