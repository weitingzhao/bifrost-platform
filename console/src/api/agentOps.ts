import type { AgentBridgeResponse, AgentDeployStartResponse, AgentDeployStatusResponse, AgentNightlyReportResponse, AgentPerformanceResponse, NightlyTriggerResponse, RetrospectiveReport, RunnerSmokeResponse } from './agentTypes'
import type { ApproveDriftProposalResponse, DriftProposal, DriftProposalsResponse } from './remediationTypes'
import { authedFetch, parseError } from './client'

export async function fetchAgentNightlyReport(): Promise<AgentNightlyReportResponse> {
  const r = await fetch('/api/v1/agent/nightly-report')
  if (!r.ok) throw await parseError('agent nightly-report', r)
  return r.json() as Promise<AgentNightlyReportResponse>
}

export async function triggerNightlyDriftScan(): Promise<NightlyTriggerResponse> {
  const r = await authedFetch('agent nightly-run', '/api/v1/agent/nightly-run', { method: 'POST' })
  return r.json() as Promise<NightlyTriggerResponse>
}

export async function fetchAgentDeployStatus(): Promise<AgentDeployStatusResponse> {
  const r = await fetch('/api/v1/agent/deploy')
  if (!r.ok) throw await parseError('agent deploy status', r)
  return r.json() as Promise<AgentDeployStatusResponse>
}

export async function startAgentDeploy(opts?: {
  target?: string
  remote?: string
}): Promise<AgentDeployStartResponse> {
  const payload: { target?: string; remote?: string } = {}
  if (opts?.target != null && opts.target.trim() !== '') payload.target = opts.target.trim()
  if (opts?.remote != null && opts.remote.trim() !== '') payload.remote = opts.remote.trim()
  const r = await authedFetch('agent deploy', '/api/v1/agent/deploy', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
  return r.json() as Promise<AgentDeployStartResponse>
}

export async function fetchAgentBridge(): Promise<AgentBridgeResponse> {
  const r = await fetch('/api/v1/agent/bridge')
  if (!r.ok) throw await parseError('agent bridge', r)
  return r.json() as Promise<AgentBridgeResponse>
}

export async function fetchDriftProposals(): Promise<DriftProposalsResponse> {
  const r = await fetch('/api/v1/agent/drift-proposals')
  if (!r.ok) throw await parseError('drift proposals', r)
  return r.json() as Promise<DriftProposalsResponse>
}

export async function approveDriftProposal(id: string): Promise<ApproveDriftProposalResponse> {
  const r = await authedFetch(
    'drift proposal approve',
    `/api/v1/agent/drift-proposals/${encodeURIComponent(id)}/approve`,
    { method: 'POST' },
  )
  return r.json() as Promise<ApproveDriftProposalResponse>
}

export async function rejectDriftProposal(id: string, note?: string): Promise<DriftProposal> {
  const r = await authedFetch(
    'drift proposal reject',
    `/api/v1/agent/drift-proposals/${encodeURIComponent(id)}/reject`,
    {
      method: 'POST',
      body: JSON.stringify({ note: note ?? '' }),
    },
  )
  return r.json() as Promise<DriftProposal>
}

export async function fetchRunnerSmoke(): Promise<RunnerSmokeResponse> {
  const r = await fetch('/api/v1/agent/smoke')
  if (!r.ok) throw new Error(`runner smoke: HTTP ${r.status}`)
  return r.json() as Promise<RunnerSmokeResponse>
}

// Hermes Gateway — Autonomous Agent

export async function fetchAgentPerformance(): Promise<AgentPerformanceResponse> {
  const r = await fetch('/api/v1/agent/governance/performance')
  if (!r.ok) throw new Error(`agent performance: HTTP ${r.status}`)
  return r.json() as Promise<AgentPerformanceResponse>
}

export async function fetchRetrospectiveReport(refresh = false): Promise<RetrospectiveReport> {
  const url = refresh
    ? '/api/v1/agent/retrospective/report?refresh=true'
    : '/api/v1/agent/retrospective/report'
  const r = await fetch(url)
  if (!r.ok) throw new Error(`retrospective report: HTTP ${r.status}`)
  return r.json() as Promise<RetrospectiveReport>
}

