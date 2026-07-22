import type { GateHistoryResponse, ReleaseGateResponse, ReleaseStateResponse, RunReleaseGateResponse, StgSmokeResponse, TierBSignoffResponse, TierBStatusResponse } from './deliveryTypes'
import { authedFetch } from './client'

export async function fetchStgSmoke(): Promise<StgSmokeResponse> {
  const r = await fetch('/api/v1/delivery/stg/smoke')
  if (!r.ok) throw new Error(`stg smoke: HTTP ${r.status}`)
  return r.json() as Promise<StgSmokeResponse>
}

export type ReleaseGateTier = 'stg' | 'prod' | 'platform-stg' | 'platform-prod'

export async function fetchReleaseGate(tier: ReleaseGateTier = 'prod'): Promise<ReleaseGateResponse> {
  const r = await fetch(`/api/v1/promote/release-gate?tier=${tier}`)
  if (!r.ok) throw new Error(`release gate: HTTP ${r.status}`)
  return r.json() as Promise<ReleaseGateResponse>
}

export async function runReleaseGate(tier: ReleaseGateTier = 'prod'): Promise<RunReleaseGateResponse> {
  const r = await authedFetch('release gate', `/api/v1/promote/release-gate?tier=${tier}`, {
    method: 'POST',
  })
  return r.json() as Promise<RunReleaseGateResponse>
}

export async function fetchGateHistory(tier: ReleaseGateTier = 'prod'): Promise<GateHistoryResponse> {
  const r = await fetch(`/api/v1/promote/gate-history?tier=${tier}`)
  if (!r.ok) throw new Error(`gate history: HTTP ${r.status}`)
  return r.json() as Promise<GateHistoryResponse>
}

export async function fetchReleaseState(tier = 'platform'): Promise<ReleaseStateResponse> {
  const r = await fetch(`/api/v1/promote/release-state?tier=${tier}`)
  if (!r.ok) throw new Error(`release state: HTTP ${r.status}`)
  return r.json() as Promise<ReleaseStateResponse>
}

export async function fetchTierBStatus(): Promise<TierBStatusResponse> {
  const r = await fetch('/api/v1/promote/tier-b')
  if (!r.ok) throw new Error(`tier b: HTTP ${r.status}`)
  return r.json() as Promise<TierBStatusResponse>
}

export async function signTierB(notes = ''): Promise<TierBSignoffResponse> {
  const r = await authedFetch('tier b signoff', '/api/v1/promote/tier-b/signoff', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ notes }),
  })
  return r.json() as Promise<TierBSignoffResponse>
}

