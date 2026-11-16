import { authedFetch } from './client'
import type { PatrolSkill, PatrolRunResult } from '@/domains/foundation/patrol-view/readings'
export * from '@/domains/foundation/patrol-view/readings'

export type PatrolTriggerStatus = 'started' | 'completed' | 'blocked'

export type PatrolTriggerResponse = {
  run_id: string
  status: PatrolTriggerStatus
  result?: PatrolRunResult
  error?: string
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
