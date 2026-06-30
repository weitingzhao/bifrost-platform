/**
 * Briefing sync automation loop — derive live step status for the UI pipeline.
 * Doctrine: briefingReconciliationCatalog.ts BRIEFING_SYNC_LOOP_STEPS
 */

import type { AgentNightlyReportResponse, DriftProposal, RemediationJob } from '@/api/types'
import { BRIEFING_SYNC_LOOP_STEPS } from '@/lib/architecture/briefingReconciliationCatalog'
import { catalogTaskById } from '@/lib/agent/agentTaskCatalog'
import {
  hasBlockingFindings,
  type ReconcileFinding,
} from '@/lib/briefing/reconcileBriefing'

export type SyncLoopStepStatus = 'ok' | 'warning' | 'active' | 'idle' | 'fail' | 'unknown'

export type BriefingSyncLoopStepView = {
  id: string
  label: string
  agentTaskLabel?: string
  status: SyncLoopStepStatus
  detail: string
  action?: 'scroll-proposals' | 'open-agent-desk'
  actionJobId?: string
}

function extractReportSection(content: string, marker: string): string {
  const idx = content.indexOf(marker)
  if (idx < 0) return ''
  const rest = content.slice(idx + marker.length)
  const next = rest.search(/\n## /)
  return next < 0 ? rest : rest.slice(0, next)
}

function layer4Section(content: string): string {
  const start = content.indexOf('## Layer 4')
  if (start < 0) return ''
  const end = content.indexOf('## Runner health', start)
  return end > start ? content.slice(start, end) : content.slice(start, start + 8000)
}

export function parseNightlyLayerResults(content: string | undefined): {
  l1: 'pass' | 'fail' | 'unknown'
  l2: 'pass' | 'fail' | 'unknown'
  l3: 'pass' | 'fail' | 'unknown'
  l4Hint: 'no_drift' | 'posted' | 'post_skipped' | 'post_failed' | 'unknown'
} {
  if (content == null || content.trim() === '') {
    return { l1: 'unknown', l2: 'unknown', l3: 'unknown', l4Hint: 'unknown' }
  }

  const l1 = extractReportSection(content, '## Layer 1 — Catalog drift scan') || extractReportSection(content, '## Layer 1')
  const l2 = extractReportSection(content, '## Layer 2 — API probe scan') || extractReportSection(content, '## Layer 2')
  const l3 = extractReportSection(content, '## Layer 3 — Semantic / spine drift scan') || extractReportSection(content, '## Layer 3')

  const layerStatus = (section: string, passPhrases: string[]): 'pass' | 'fail' | 'unknown' => {
    if (section.trim() === '') return 'unknown'
    if (/Findings:\s*[1-9]\d*/.test(section)) return 'fail'
    if (/### Failures/.test(section)) return 'fail'
    if (passPhrases.some(p => section.includes(p))) return 'pass'
    if (/Findings:\s*0/.test(section)) return 'pass'
    return 'unknown'
  }

  let l4Hint: 'no_drift' | 'posted' | 'post_skipped' | 'post_failed' | 'unknown' = 'unknown'
  const l4Block = layer4Section(content)
  if (l4Block.includes('No drift — skipping Layer 4 proposal') || content.includes('No drift — skipping Layer 4 proposal')) {
    l4Hint = 'no_drift'
  } else if (l4Block.includes('SKIP proposal POST')) {
    l4Hint = 'post_skipped'
  } else if (l4Block.includes('POST failed')) {
    l4Hint = 'post_failed'
  } else if (l4Block.includes('Owner approval: Ops Console')) {
    l4Hint = 'posted'
  }

  return {
    l1: layerStatus(l1, ['No deterministic drift detected.']),
    l2: layerStatus(l2, ['All API probes passed.']),
    l3: layerStatus(l3, ['Live spine matches static catalog authorities.']),
    l4Hint,
  }
}

function latestJobByScope(jobs: RemediationJob[], scope: string): RemediationJob | null {
  const matches = jobs.filter(j => j.scope === scope)
  if (matches.length === 0) return null
  return [...matches].sort((a, b) => Date.parse(b.created_at) - Date.parse(a.created_at))[0]
}

function jobStatusToLoop(status: RemediationJob['status']): SyncLoopStepStatus {
  if (status === 'done') return 'ok'
  if (status === 'running') return 'active'
  if (status === 'failed' || status === 'cancelled') return 'fail'
  return 'unknown'
}

/** Build the five-step pipeline view from live Console + agent data. */
export function buildBriefingSyncLoopSteps(input: {
  reconcileFindings: ReconcileFinding[]
  nightlyReport?: AgentNightlyReportResponse
  proposals: DriftProposal[]
  remediationJobs: RemediationJob[]
}): BriefingSyncLoopStepView[] {
  const { reconcileFindings, nightlyReport, proposals, remediationJobs } = input
  const layers = parseNightlyLayerResults(nightlyReport?.content)
  const pending = proposals.filter(p => p.status === 'pending_approval')
  const latestProposal = [...proposals].sort(
    (a, b) => Date.parse(b.created_at) - Date.parse(a.created_at),
  )[0]
  const autofixJob = latestJobByScope(remediationJobs, 'nightly-drift-autofix')
  const briefJob = latestJobByScope(remediationJobs, 'nightly-drift-briefing')

  const specs = BRIEFING_SYNC_LOOP_STEPS

  // 1 — Runtime SYNC
  let runtimeStatus: SyncLoopStepStatus = 'unknown'
  let runtimeDetail = 'Spine not loaded'
  if (reconcileFindings.length === 0) {
    runtimeStatus = 'ok'
    runtimeDetail = 'Reconcile gate clear — matches nightly L3 when scan runs'
  } else if (hasBlockingFindings(reconcileFindings)) {
    runtimeStatus = 'fail'
    runtimeDetail = `${reconcileFindings.length} finding(s) — pack HARD-BLOCKED`
  } else {
    runtimeStatus = 'warning'
    runtimeDetail = `${reconcileFindings.length} warning(s) — BRIEFING_STALE banner`
  }

  // 2 — Nightly L1–L3
  let nightlyStatus: SyncLoopStepStatus = 'unknown'
  let nightlyDetail = 'No nightly report yet'
  if (nightlyReport?.available && nightlyReport.content != null) {
    const fails = [layers.l1, layers.l2, layers.l3].filter(s => s === 'fail').length
    const unknowns = [layers.l1, layers.l2, layers.l3].filter(s => s === 'unknown').length
    if (fails > 0) {
      nightlyStatus = 'fail'
      nightlyDetail = `L1=${layers.l1} · L2=${layers.l2} · L3=${layers.l3} — drift detected`
    } else if (unknowns === 3) {
      nightlyStatus = 'unknown'
      nightlyDetail = 'Report present — could not parse layer results'
    } else {
      nightlyStatus = 'ok'
      nightlyDetail = 'Layer 1–3 passed — briefing reconcile parity (L3)'
    }
    if (nightlyReport.generated_at != null) {
      nightlyDetail += ` · ${new Date(nightlyReport.generated_at).toLocaleString()}`
    }
    if (briefJob != null) {
      nightlyDetail += ` · brief job ${briefJob.status}`
    }
  } else if (nightlyReport != null && !nightlyReport.available) {
    nightlyStatus = 'idle'
    nightlyDetail = nightlyReport.hint ?? 'Awaiting first Mac Mini nightly run'
  }

  // 3 — L4 proposal
  let proposalStatus: SyncLoopStepStatus = 'idle'
  let proposalDetail = 'Skipped when L1–L3 pass'
  if (layers.l4Hint === 'post_skipped') {
    proposalStatus = 'warning'
    proposalDetail = 'Drift detected but proposal POST skipped — check agent token'
  } else if (pending.length > 0) {
    proposalStatus = 'active'
    proposalDetail = `${pending.length} proposal(s) awaiting Owner review`
  } else if (latestProposal != null) {
    proposalStatus =
      latestProposal.status === 'failed'
        ? 'fail'
        : latestProposal.status === 'done'
          ? 'ok'
          : 'unknown'
    proposalDetail = `Latest: ${latestProposal.status} · ${latestProposal.findings_count} findings`
  } else if (layers.l4Hint === 'no_drift') {
    proposalStatus = 'idle'
    proposalDetail = 'No drift — Layer 4 did not create a proposal'
  } else if (layers.l4Hint === 'post_failed') {
    proposalStatus = 'fail'
    proposalDetail = 'POST to drift-proposals failed — check agent token / API reachability'
  } else if (layers.l4Hint === 'posted') {
    proposalStatus = pending.length > 0 ? 'active' : 'warning'
    proposalDetail =
      pending.length > 0
        ? `${pending.length} proposal(s) awaiting Owner review`
        : 'Proposal on remote platform-api — open K3s Console or align PLATFORM_API_URL'
  }

  // 4 — Owner approval
  let approvalStatus: SyncLoopStepStatus = 'idle'
  let approvalDetail = 'No approval needed when scan is clean'
  if (pending.length > 0) {
    approvalStatus = 'active'
    approvalDetail = `${pending.length} pending — approve or reject below`
  } else if (latestProposal?.status === 'rejected') {
    approvalStatus = 'ok'
    approvalDetail = `Last proposal rejected · ${latestProposal.id.slice(0, 8)}`
  } else if (
    latestProposal != null &&
    ['approved', 'running', 'done'].includes(latestProposal.status)
  ) {
    approvalStatus = 'ok'
    approvalDetail = `Approved · ${latestProposal.approved_at != null ? new Date(latestProposal.approved_at).toLocaleString() : latestProposal.status}`
  }

  // 5 — Drift fix
  let fixStatus: SyncLoopStepStatus = 'idle'
  let fixDetail = 'Runs after Owner approves L4 proposal'
  let fixJobId: string | undefined
  if (autofixJob != null) {
    fixStatus = jobStatusToLoop(autofixJob.status)
    fixJobId = autofixJob.id
    fixDetail = `Job ${autofixJob.id.slice(0, 8)} · ${autofixJob.status}`
  } else if (latestProposal?.remediation_job_id != null) {
    fixJobId = latestProposal.remediation_job_id
    fixDetail = `Linked to proposal · open Agent Desk`
    fixStatus = 'active'
  }

  const statuses: SyncLoopStepStatus[] = [
    runtimeStatus,
    nightlyStatus,
    proposalStatus,
    approvalStatus,
    fixStatus,
  ]
  const details = [runtimeDetail, nightlyDetail, proposalDetail, approvalDetail, fixDetail]

  return specs.map((spec, i) => {
    const agentTask = spec.agentTaskId != null ? catalogTaskById(spec.agentTaskId) : undefined
    const step: BriefingSyncLoopStepView = {
      id: spec.id,
      label: spec.label,
      agentTaskLabel: agentTask?.label,
      status: statuses[i] ?? 'unknown',
      detail: details[i] ?? '',
    }
    if (spec.id === 'owner-approval' && pending.length > 0) {
      step.action = 'scroll-proposals'
    }
    if (spec.id === 'drift-fix' && fixJobId != null) {
      step.action = 'open-agent-desk'
      step.actionJobId = fixJobId
    }
    return step
  })
}

export function syncLoopStatusTagVariant(
  status: SyncLoopStepStatus,
): 'success' | 'warning' | 'neutral' | 'danger' {
  switch (status) {
    case 'ok':
      return 'success'
    case 'warning':
      return 'warning'
    case 'active':
      return 'neutral'
    case 'fail':
      return 'danger'
    default:
      return 'neutral'
  }
}

export function syncLoopStatusLabel(status: SyncLoopStepStatus): string {
  switch (status) {
    case 'ok':
      return 'OK'
    case 'warning':
      return 'WARN'
    case 'active':
      return 'ACTIVE'
    case 'idle':
      return 'IDLE'
    case 'fail':
      return 'FAIL'
    default:
      return '—'
  }
}
