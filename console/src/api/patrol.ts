import { authedFetch } from './client'

export type PatrolTrustLevel = 'L0' | 'L1' | 'L2'
export type PatrolRunResult = 'success' | 'failure' | 'skipped' | 'escalated' | 'running'
export type PatrolTrigger = 'cron' | 'manual' | 'webhook'
export type PatrolTriggerStatus = 'started' | 'completed' | 'blocked'

export type PatrolSkill = {
  id: string
  name: string
  description: string
  schedule: string
  prompt_template: string
  mcp_tools: string[]
  trust_level: PatrolTrustLevel
  scope: string
  timeout_seconds: number
  enabled: boolean
  last_run_at?: string
  last_result?: PatrolRunResult
  next_run_at?: string
  cron_actuation?: string
}

export type PatrolRun = {
  id: string
  skill_id: string
  skill_name: string
  trigger: PatrolTrigger
  started_at: string
  finished_at?: string
  duration_ms?: number
  result: PatrolRunResult
  evidence?: string
  error?: string
}

export type PatrolSkillsResponse = {
  skills: PatrolSkill[]
}

export type PatrolRunsResponse = {
  runs: PatrolRun[]
  total: number
}

export type PatrolTriggerResponse = {
  run_id: string
  status: PatrolTriggerStatus
  result?: PatrolRunResult
  error?: string
}

export async function fetchPatrolSkills(): Promise<PatrolSkillsResponse> {
  const r = await fetch('/api/v1/patrol/skills')
  if (!r.ok) throw new Error(`patrol skills: HTTP ${r.status}`)
  return r.json() as Promise<PatrolSkillsResponse>
}

export async function fetchPatrolSkill(id: string): Promise<PatrolSkill> {
  const r = await fetch(`/api/v1/patrol/skills/${encodeURIComponent(id)}`)
  if (!r.ok) throw new Error(`patrol skill: HTTP ${r.status}`)
  return r.json() as Promise<PatrolSkill>
}

export async function fetchPatrolRuns(limit = 50): Promise<PatrolRunsResponse> {
  const r = await fetch(`/api/v1/patrol/runs?limit=${limit}`)
  if (!r.ok) throw new Error(`patrol runs: HTTP ${r.status}`)
  return r.json() as Promise<PatrolRunsResponse>
}

export async function enablePatrolSkill(id: string, enabled: boolean): Promise<PatrolSkill> {
  const r = await authedFetch('patrol enable', `/api/v1/patrol/skills/${encodeURIComponent(id)}/enable`, {
    method: 'PUT',
    body: JSON.stringify({ enabled }),
  })
  return r.json() as Promise<PatrolSkill>
}

export async function triggerPatrolSkill(id: string): Promise<PatrolTriggerResponse> {
  const r = await authedFetch('patrol trigger', `/api/v1/patrol/trigger/${encodeURIComponent(id)}`, {
    method: 'POST',
  })
  return r.json() as Promise<PatrolTriggerResponse>
}
