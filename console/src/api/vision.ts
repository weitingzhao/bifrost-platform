import type { RunVisionV1GateResponse, VisionV1GateResponse } from './deliveryTypes'
import { authedFetch } from './client'

export async function fetchVisionV1Gate(): Promise<VisionV1GateResponse> {
  const r = await fetch('/api/v1/vision/v1/gate')
  if (!r.ok) throw new Error(`vision v1 gate: HTTP ${r.status}`)
  return r.json() as Promise<VisionV1GateResponse>
}

export async function runVisionV1Gate(): Promise<RunVisionV1GateResponse> {
  const r = await authedFetch('vision v1 gate', '/api/v1/vision/v1/gate', { method: 'POST' })
  return r.json() as Promise<RunVisionV1GateResponse>
}

export async function signVisionV1(notes = ''): Promise<RunVisionV1GateResponse> {
  const r = await authedFetch('vision v1 signoff', '/api/v1/vision/v1/signoff', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ notes }),
  })
  return r.json() as Promise<RunVisionV1GateResponse>
}

export async function fetchVisionS3Gate(): Promise<VisionV1GateResponse> {
  const r = await fetch('/api/v1/vision/s3/gate')
  if (!r.ok) throw new Error(`vision s3 gate: HTTP ${r.status}`)
  return r.json() as Promise<VisionV1GateResponse>
}

export async function runVisionS3Gate(): Promise<RunVisionV1GateResponse> {
  const r = await authedFetch('vision s3 gate', '/api/v1/vision/s3/gate', { method: 'POST' })
  return r.json() as Promise<RunVisionV1GateResponse>
}

export async function signVisionS3(notes = ''): Promise<RunVisionV1GateResponse> {
  const r = await authedFetch('vision s3 signoff', '/api/v1/vision/s3/signoff', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ notes }),
  })
  return r.json() as Promise<RunVisionV1GateResponse>
}

export async function fetchVisionV2Gate(): Promise<VisionV1GateResponse> {
  const r = await fetch('/api/v1/vision/v2/gate')
  if (!r.ok) throw new Error(`vision v2 gate: HTTP ${r.status}`)
  return r.json() as Promise<VisionV1GateResponse>
}

export async function runVisionV2Gate(): Promise<RunVisionV1GateResponse> {
  const r = await authedFetch('vision v2 gate', '/api/v1/vision/v2/gate', { method: 'POST' })
  return r.json() as Promise<RunVisionV1GateResponse>
}

export async function signVisionV2(notes = ''): Promise<RunVisionV1GateResponse> {
  const r = await authedFetch('vision v2 signoff', '/api/v1/vision/v2/signoff', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ notes }),
  })
  return r.json() as Promise<RunVisionV1GateResponse>
}

export async function fetchVisionV3Gate(): Promise<VisionV1GateResponse> {
  const r = await fetch('/api/v1/vision/v3/gate')
  if (!r.ok) throw new Error(`vision v3 gate: HTTP ${r.status}`)
  return r.json() as Promise<VisionV1GateResponse>
}

export async function runVisionV3Gate(): Promise<RunVisionV1GateResponse> {
  const r = await authedFetch('vision v3 gate', '/api/v1/vision/v3/gate', { method: 'POST' })
  return r.json() as Promise<RunVisionV1GateResponse>
}

export async function signVisionV3(notes = ''): Promise<RunVisionV1GateResponse> {
  const r = await authedFetch('vision v3 signoff', '/api/v1/vision/v3/signoff', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ notes }),
  })
  return r.json() as Promise<RunVisionV1GateResponse>
}

export async function fetchVisionV4Gate(): Promise<VisionV1GateResponse> {
  const r = await fetch('/api/v1/vision/v4/gate')
  if (!r.ok) throw new Error(`vision v4 gate: HTTP ${r.status}`)
  return r.json() as Promise<VisionV1GateResponse>
}

export async function runVisionV4Gate(): Promise<RunVisionV1GateResponse> {
  const r = await authedFetch('vision v4 gate', '/api/v1/vision/v4/gate', { method: 'POST' })
  return r.json() as Promise<RunVisionV1GateResponse>
}

export async function signVisionV4(notes = ''): Promise<RunVisionV1GateResponse> {
  const r = await authedFetch('vision v4 signoff', '/api/v1/vision/v4/signoff', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ notes }),
  })
  return r.json() as Promise<RunVisionV1GateResponse>
}

export async function fetchVisionV5Gate(): Promise<VisionV1GateResponse> {
  const r = await fetch('/api/v1/vision/v5/gate')
  if (!r.ok) throw new Error(`vision v5 gate: HTTP ${r.status}`)
  return r.json() as Promise<VisionV1GateResponse>
}

export async function runVisionV5Gate(): Promise<RunVisionV1GateResponse> {
  const r = await authedFetch('vision v5 gate', '/api/v1/vision/v5/gate', { method: 'POST' })
  return r.json() as Promise<RunVisionV1GateResponse>
}

export async function signVisionV5(notes = ''): Promise<RunVisionV1GateResponse> {
  const r = await authedFetch('vision v5 signoff', '/api/v1/vision/v5/signoff', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ notes }),
  })
  return r.json() as Promise<RunVisionV1GateResponse>
}

/** All Vision gates for Delivery Board signed/total (V5 → V1 + S3). */
export const VISION_PROGRAM_GATES_QUERY_KEY = ['vision', 'program', 'gates'] as const

export async function fetchVisionProgramGates(): Promise<VisionV1GateResponse[]> {
  return Promise.all([
    fetchVisionV5Gate(),
    fetchVisionV4Gate(),
    fetchVisionV3Gate(),
    fetchVisionV2Gate(),
    fetchVisionS3Gate(),
    fetchVisionV1Gate(),
  ])
}

