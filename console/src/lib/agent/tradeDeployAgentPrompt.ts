import type { DeliveryPipelineRunView, ReleaseGateResponse, StgSmokeResponse, SupplyChainResponse, TierBStatusResponse } from '@/api/deliveryTypes'
import type { MatrixResponse } from '@/api/matrixTypes'
import { DELIVER_STG_PIPELINE } from '@/lib/delivery/deliverStgPhases'
import { deliveryTargetById } from '@/lib/delivery/deliveryTargets'

export const TRADE_DEPLOY_SCOPE = 'trade-deploy'

export const TRADE_DEPLOY_AGENT_PROMPT = [
  'Deploy the Bifrost Trade stack through STG → PROD using Tekton deliver pipelines.',
  'Scan Gitea mirror state, refresh Dockerfile ConfigMaps if needed, run bifrost-deliver-stg, verify STG smoke + release gate,',
  'then — only after operator approval — run bifrost-deliver-prod with the SAME revision as STG.',
  'Do NOT enable live trading (daemon execution remains blocked per D10).',
].join(' ')

export interface TradeDeployPromptContext {
  stgRun?: DeliveryPipelineRunView
  prodRun?: DeliveryPipelineRunView
  stgGate?: ReleaseGateResponse
  prodGate?: ReleaseGateResponse
  stgSmoke?: StgSmokeResponse
  tierB?: TierBStatusResponse
  supplyChain?: SupplyChainResponse
  matrices?: MatrixResponse[]
  /** Where the operator clicked AI Deploy (defaults to Control Room Launch Pad). */
  operatorSurface?: string
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
    failed_checks: gate.checks?.filter(c => c.reachability === 'fail').map(c => c.label) ?? [],
  }
}

function tradeEnvSummary(matrices: MatrixResponse[] | undefined) {
  if (matrices == null) return []
  return matrices
    .filter(m => ['dev', 'stg', 'prod'].includes(m.environment))
    .map(m => {
      const failing = m.targets.filter(t => t.reachability === 'fail').length
      const degraded = m.targets.filter(t => t.reachability === 'degraded').length
      return {
        environment: m.environment,
        failing,
        degraded,
        total: m.targets.length,
      }
    })
}

export function buildTradeDeployPrompt(ctx: TradeDeployPromptContext): string {
  const stgTarget = deliveryTargetById('trade-stg')
  const prodTarget = deliveryTargetById('trade-prod')
  const surface = ctx.operatorSurface ?? 'Control Room Launch Pad'
  const snapshot = {
    stg_pipeline: DELIVER_STG_PIPELINE,
    prod_pipeline: prodTarget.pipeline,
    stg_namespace: stgTarget.namespace,
    prod_namespace: prodTarget.namespace,
    stg_latest_run: summarizeRun(ctx.stgRun),
    prod_latest_run: summarizeRun(ctx.prodRun),
    stg_gate: summarizeGate(ctx.stgGate),
    prod_gate: summarizeGate(ctx.prodGate),
    stg_smoke: ctx.stgSmoke ?? null,
    tier_b: ctx.tierB ?? null,
    supply_chain: ctx.supplyChain
      ? {
          tracked_repos: ctx.supplyChain.tracked_repos?.length ?? 0,
          dockerfile_cms: ctx.supplyChain.dockerfile_configmaps?.filter(cm => cm.present).length ?? 0,
        }
      : null,
    trade_matrix: tradeEnvSummary(ctx.matrices),
  }

  return [
    TRADE_DEPLOY_AGENT_PROMPT,
    '',
    `## Operator context (${surface} at task start)`,
    surface === 'Deploy Satellite page'
      ? 'The operator clicked **AI Deploy** on the Deploy Satellite page. Use the snapshot below plus live MCP tools.'
      : 'The operator clicked **Agent Deploy** on the Control Room Launch Pad. Use the snapshot below plus live MCP tools.',
    '',
    '```json',
    JSON.stringify(snapshot, null, 2),
    '```',
    '',
    '## Trade deploy workflow (execute in order)',
    '1. Call get_supply_chain (or fetch supply chain) — confirm Gitea mirrors + Dockerfile ConfigMaps (4/4) before build.',
    '2. Call get_delivery_revisions for trade repos; confirm revision (default **main** unless operator specified).',
    '3. Call start_pipeline_run with pipeline="bifrost-deliver-stg" and revision.',
    '4. Poll get_pipeline_runs until STG run succeeds or fails; on ImagePullBackOff in cluster, diagnose registry + rollout.',
    '5. Call get_stg_smoke — all checks must pass before PROD.',
    '6. Call run_release_gate tier="stg" — resolve blockers before PROD.',
    '7. Request operator approval before PROD deploy.',
    '8. Call start_pipeline_run with pipeline="bifrost-deliver-prod" and the **same revision** as STG.',
    '9. Poll prod run, then run_release_gate tier="prod".',
    '10. On build/rollout/gitops failure needing repo fix: spawn_trade_release_fix(diagnosis) — one escalation per phase.',
    '',
    '## Reminders',
    '- Registry: 192.168.10.73:30500 — kubelet pulls via NodePort, not in-cluster DNS.',
    '- Dev overlay uses :stg tags; prod uses :prod — deliver-stg rebuilds :stg, deliver-prod rebuilds :prod.',
    '- Do NOT scale daemon for live trading; D10 execution freeze remains in effect.',
    '- Request operator approval before each gate and before PROD deploy.',
  ].join('\n')
}
