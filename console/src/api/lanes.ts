import { getPlatformOperatorToken } from '@/lib/platformAuth'
import type { WorkLane } from '@/lib/briefing/workLanes'

export interface LaneApiRecord {
  id: string
  track: string
  component_line: string
  track_type: string
  label: string
  short_label: string
  description: string
  agent_mode: string
  work_intent: string
}

export interface LanesListResponse {
  version: string
  lanes: LaneApiRecord[]
}

export interface CreateLaneRequest {
  id: string
  track: string
  component_line: string
  track_type: string
  label: string
  short_label: string
  description: string
  agent_mode: string
  work_intent: string
}

/** Mutable fields for PATCH /api/v1/lanes/{id}. Empty/omitted = unchanged. */
export interface PatchLaneRequest {
  track?: string
  component_line?: string
  track_type?: string
  short_label?: string
  description?: string
  agent_mode?: string
  work_intent?: string
}

async function parseError(prefix: string, r: Response): Promise<Error> {
  let detail = `HTTP ${r.status}`
  try {
    const body = (await r.json()) as { error?: string; message?: string }
    detail = body.error ?? body.message ?? detail
  } catch {
    // keep status detail
  }
  return new Error(`${prefix}: ${detail}`)
}

export function mapLaneApiToWorkLane(r: LaneApiRecord): WorkLane {
  return {
    id: r.id,
    track: r.track as WorkLane['track'],
    componentLine: r.component_line as WorkLane['componentLine'],
    trackType: r.track_type as WorkLane['trackType'],
    label: r.label,
    shortLabel: r.short_label,
    description: r.description,
    agentMode: r.agent_mode as WorkLane['agentMode'],
    workIntent: r.work_intent as WorkLane['workIntent'],
  }
}

export async function fetchLanes(): Promise<LanesListResponse> {
  const r = await fetch('/api/v1/lanes')
  if (!r.ok) throw await parseError('lanes', r)
  return r.json() as Promise<LanesListResponse>
}

export async function createLane(body: CreateLaneRequest): Promise<LaneApiRecord> {
  const token = getPlatformOperatorToken()
  const headers = new Headers({ 'Content-Type': 'application/json' })
  if (token !== '') headers.set('Authorization', `Bearer ${token}`)
  const r = await fetch('/api/v1/lanes', {
    method: 'POST',
    headers,
    body: JSON.stringify(body),
  })
  if (!r.ok) throw await parseError('create lane', r)
  return r.json() as Promise<LaneApiRecord>
}

export async function patchLane(
  id: string,
  body: PatchLaneRequest,
): Promise<LaneApiRecord> {
  const token = getPlatformOperatorToken()
  const headers = new Headers({ 'Content-Type': 'application/json' })
  if (token !== '') headers.set('Authorization', `Bearer ${token}`)
  const r = await fetch(`/api/v1/lanes/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    headers,
    body: JSON.stringify(body),
  })
  if (!r.ok) throw await parseError('patch lane', r)
  return r.json() as Promise<LaneApiRecord>
}

export const LANES_QUERY_KEY = ['lanes'] as const
