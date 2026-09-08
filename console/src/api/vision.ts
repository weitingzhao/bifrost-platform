import type { RunVisionV1GateResponse, VisionV1GateResponse } from './deliveryTypes'
import { authedFetch } from './client'

/**
 * Vision gates, in the order the Delivery Board and the gate panels show them.
 *
 * There used to be three functions per gate — eighteen bodies that differed only
 * in a URL segment — and six components around them that differed in a title.
 * Copying is what let the V1 panel gain a program-gates invalidation on sign-off
 * that the other five never got, so the Board's signed/total stayed stale after
 * signing anything but V1.
 */
export const VISION_GATE_IDS = ['v5', 'v4', 'v3', 'v2', 's3', 'v1'] as const

export type VisionGateId = (typeof VISION_GATE_IDS)[number]

export async function fetchVisionGate(id: VisionGateId): Promise<VisionV1GateResponse> {
  const r = await fetch(`/api/v1/vision/${id}/gate`)
  if (!r.ok) throw new Error(`vision ${id} gate: HTTP ${r.status}`)
  return r.json() as Promise<VisionV1GateResponse>
}

export async function runVisionGate(id: VisionGateId): Promise<RunVisionV1GateResponse> {
  const r = await authedFetch(`vision ${id} gate`, `/api/v1/vision/${id}/gate`, { method: 'POST' })
  return r.json() as Promise<RunVisionV1GateResponse>
}

export async function signVisionGate(id: VisionGateId, notes = ''): Promise<RunVisionV1GateResponse> {
  const r = await authedFetch(`vision ${id} signoff`, `/api/v1/vision/${id}/signoff`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ notes }),
  })
  return r.json() as Promise<RunVisionV1GateResponse>
}

/** The query key one gate's panel owns. */
export function visionGateQueryKey(id: VisionGateId) {
  return ['vision', id, 'gate'] as const
}

/** All Vision gates for Delivery Board signed/total (V5 → V1 + S3). */
export const VISION_PROGRAM_GATES_QUERY_KEY = ['vision', 'program', 'gates'] as const

export async function fetchVisionProgramGates(): Promise<VisionV1GateResponse[]> {
  return Promise.all(VISION_GATE_IDS.map(fetchVisionGate))
}
