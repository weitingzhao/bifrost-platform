import type { HermesActuationLevel, HermesExecutionsResponse, HermesGatewayHealth, HermesReadinessResponse, HermesSchedulesResponse, HermesSkillsResponse } from './agentTypes'
import { authedFetch } from './client'

export async function fetchHermesReadiness(): Promise<HermesReadinessResponse> {
  const r = await fetch('/api/v1/agent/hermes/readiness')
  if (!r.ok) throw new Error(`hermes-readiness: HTTP ${r.status}`)
  return r.json() as Promise<HermesReadinessResponse>
}

export async function fetchHermesGatewayHealth(): Promise<HermesGatewayHealth> {
  const r = await fetch('/api/v1/agent/hermes/health')
  if (!r.ok) throw new Error(`hermes health: HTTP ${r.status}`)
  return r.json() as Promise<HermesGatewayHealth>
}

export async function fetchHermesSkills(): Promise<HermesSkillsResponse> {
  const r = await fetch('/api/v1/agent/skills')
  if (!r.ok) throw new Error(`hermes skills: HTTP ${r.status}`)
  return r.json() as Promise<HermesSkillsResponse>
}

export async function fetchHermesSchedules(): Promise<HermesSchedulesResponse> {
  const r = await fetch('/api/v1/agent/schedules')
  if (!r.ok) throw new Error(`hermes schedules: HTTP ${r.status}`)
  return r.json() as Promise<HermesSchedulesResponse>
}

export async function fetchHermesExecutions(limit = 50): Promise<HermesExecutionsResponse> {
  const r = await fetch(`/api/v1/agent/executions?limit=${limit}`)
  if (!r.ok) throw new Error(`hermes executions: HTTP ${r.status}`)
  return r.json() as Promise<HermesExecutionsResponse>
}

export async function updateSkillActuationLevel(
  skillId: string,
  level: HermesActuationLevel,
): Promise<void> {
  await authedFetch('skill actuation level', `/api/v1/agent/skills/${skillId}/actuation-level`, {
    method: 'PUT',
    body: JSON.stringify({ level }),
  })
}

// Agent Governance — Flight Director

