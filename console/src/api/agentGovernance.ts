import type { CapabilityMapResponse, FlightDirectorSnapshotResponse, TrustMatrixEntry, TrustMatrixResponse, TrustOverrideRequest } from './agentTypes'
import { authedFetch } from './client'

export async function fetchTrustMatrix(): Promise<TrustMatrixResponse> {
  const r = await fetch('/api/v1/agent/governance/trust-matrix')
  if (!r.ok) throw new Error(`trust matrix: HTTP ${r.status}`)
  return r.json() as Promise<TrustMatrixResponse>
}

export async function fetchCapabilityMap(): Promise<CapabilityMapResponse> {
  const r = await fetch('/api/v1/agent/governance/capability-map')
  if (!r.ok) throw new Error(`capability-map: HTTP ${r.status}`)
  return r.json() as Promise<CapabilityMapResponse>
}

export async function fetchFlightDirectorSnapshot(): Promise<FlightDirectorSnapshotResponse> {
  const r = await fetch('/api/v1/agent/governance/snapshot')
  if (!r.ok) throw new Error(`flight-director-snapshot: HTTP ${r.status}`)
  return r.json() as Promise<FlightDirectorSnapshotResponse>
}

export async function putTrustOverride(
  skillId: string,
  body: TrustOverrideRequest,
): Promise<TrustMatrixEntry> {
  const r = await authedFetch('trust override', `/api/v1/agent/governance/trust-overrides/${skillId}`, {
    method: 'PUT',
    body: JSON.stringify(body),
  })
  return r.json() as Promise<TrustMatrixEntry>
}

// Retrospective Agent — cross-job pattern analysis

