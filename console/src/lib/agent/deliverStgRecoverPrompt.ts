import type { StgSmokeResponse, SupplyChainResponse } from '@/api/deliveryTypes'
import {
  formatPipelineRunStatus,
  isPipelineRunFailed,
  platformDeliverAskContext,
  TRADE_STG_ASK_CONTEXT,
} from '@/lib/delivery/pipelineRunAskPack'

import { DELIVER_STG_RECOVER_SCOPE } from '@/lib/agent/agentScopes'

export const DELIVER_STG_RECOVER_PLAYBOOK_ID = 'deliver-stg-recover'
export { DELIVER_STG_RECOVER_SCOPE }

export function stgSmokeSummary(stg?: StgSmokeResponse): { ok: number; total: number; allOk: boolean } {
  const total = stg?.targets.length ?? 0
  const ok = stg?.targets.filter(t => t.reachability === 'ok').length ?? 0
  return { ok, total, allOk: total > 0 && ok === total }
}

/** True when last PipelineRun failed but STG runtime probes are green — not a node/K8s outage. */
export function isDeliverStgStaleFailure(
  supply?: SupplyChainResponse,
  stg?: StgSmokeResponse,
): boolean {
  const last = supply?.last_deliver_run
  if (last == null) return false
  if (!isPipelineRunFailed(last)) return false
  return stgSmokeSummary(stg).allOk
}

export function buildDeliverStgRecoverPrompt(input: {
  supply?: SupplyChainResponse
  stgSmoke?: StgSmokeResponse
  /** Trade STG (default) vs platform deliver — selects ask-pack context. */
  pipeline?: 'bifrost-deliver-stg' | 'bifrost-deliver-platform'
}): string {
  const pipeline = input.pipeline ?? 'bifrost-deliver-stg'
  const last = input.supply?.last_deliver_run
  const smoke = stgSmokeSummary(input.stgSmoke)
  const stale = isDeliverStgStaleFailure(input.supply, input.stgSmoke)
  const lastSuccess = input.supply?.last_deliver_success

  const askContext =
    pipeline === 'bifrost-deliver-platform'
      ? platformDeliverAskContext({ shortLabel: 'STG', namespace: 'bifrost-platform-stg' })
      : TRADE_STG_ASK_CONTEXT

  const runBlock =
    last != null
      ? [
          `- pipeline: ${pipeline}`,
          `- run: ${last.name}`,
          `- status: ${formatPipelineRunStatus(last)} (raw: ${last.status})`,
          last.reason != null && last.reason !== '' ? `- reason: ${last.reason}` : null,
          last.revision != null && last.revision !== '' ? `- revision: ${last.revision}` : null,
          last.start_time != null ? `- started: ${last.start_time}` : null,
          last.completion_time != null ? `- completed: ${last.completion_time}` : null,
        ]
          .filter((line): line is string => line != null)
          .join('\n')
      : '- No recent deliver-stg PipelineRun found in Tekton (cicd namespace).'

  const smokeLine =
    smoke.total > 0
      ? `${smoke.ok}/${smoke.total} targets OK`
      : 'STG smoke not probed — call get_stg_smoke (MCP)'

  return [
    `Playbook: **${DELIVER_STG_RECOVER_PLAYBOOK_ID}** (L1 — Ops mode)`,
    '',
    '## Task',
    stale
      ? 'Last bifrost-deliver-stg PipelineRun **failed** but STG runtime smoke is **green**. This is a **delivery/GitOps pipeline** problem — not a cluster node outage. Diagnose the failing Tekton task, fix repo/manifest/GitOps, then re-run deliver-stg.'
      : 'Diagnose and recover a failed or stuck bifrost-deliver-stg PipelineRun. Fix root cause before re-run; do not enable live trading (D10 freeze).',
    'Respond in Chinese. Prefer Ops Console / platform-api / MCP over manual ssh.',
    '',
    '## Classification',
    stale
      ? '- **Track:** playbook (delivery pipeline) — distinct from Cluster failing pods / NotReady nodes'
      : '- **Track:** playbook (delivery pipeline)',
    `- **STG smoke:** ${smokeLine}${stale ? ' — runtime nominal despite pipeline fail' : ''}`,
    lastSuccess != null
      ? `- **Last success:** ${lastSuccess.name} (${formatPipelineRunStatus(lastSuccess)})`
      : '- **Last success:** none recorded — may need first green deliver-stg after fix',
    '',
    '## Last PipelineRun',
    runBlock,
    '',
    '## L1 workflow (in order)',
    '1. `get_delivery_run_logs` (or get_pipeline_runs) — identify failing Tekton **task** and step (clone, kaniko, rollout, gitops-sync, verify-stg).',
    '2. **If rollout failed:** inspect rollout pod logs for `ROLL_FAILED deployment/xxx`; describe failing Deployment; safe rollout_restart only after root cause identified. Do NOT cordon nodes when STG smoke is green.',
    '3. **If gitops-sync failed OR Argo ComparisonError (programs/ missing):** call `get_gitops_apps`, fix manifest in GitOps repo, `gitops_sync_app`, or escalate to `gitops-config-repair` / `spawn_trade_release_fix` for repo commits.',
    '3b. **If PipelineRun is terminal (Failed) and remnant pods inflate failing_pods:** `delete_pipeline_run` with operator approval to clean up the CR + pods.',
    '4. If ImagePullBackOff after rollout: confirm Kaniko pushed to registry.cicd and image tag matches deployment.',
    '5. If ConfigMap/Secret missing: restore in GitOps repo — do not patch prod secrets blindly.',
    '6. If code/manifest fix needed in trade-infra or trade-* repos: call `spawn_trade_release_fix` with diagnosis — do not repeat cluster-auto.',
    '7. Re-run: `start_pipeline_run` pipeline=`bifrost-deliver-stg` revision=`main` (after fix).',
    '8. Confirm: `get_stg_smoke` all green + `verify_mission_snapshot` release signal no longer fail.',
    '',
    `## Pipeline reference (${askContext.pipelineTitle})`,
    ...askContext.pipelineOrder.map(t => `- ${t}`),
    '',
    '## Cluster signals (do not confuse with pipeline fail)',
    ...askContext.clusterSignals.map(s => `- ${s}`),
    `- STG smoke detail: ${smokeLine}`,
    '',
    '## Must-not',
    '- Do not cordon nodes or treat as hardware failure when smoke is green and nodes are Ready.',
    '- Do not enable live trading / scale daemon for execution (D10 BLOCKED).',
    '- Do not bypass release gates.',
    '',
    'Before closing: verify_mission_snapshot → post_fix_verification.passed = true.',
  ].join('\n')
}
