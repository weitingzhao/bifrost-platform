import type { BuildPhaseGateResponse, RunBuildPhaseGateResponse } from './opsContextTypes'
import { authedFetch } from './client'

export async function fetchBuildPhases(): Promise<BuildPhaseGateResponse[]> {
  const r = await fetch('/api/v1/build-phase')
  if (!r.ok) throw new Error(`build phases: HTTP ${r.status}`)
  return r.json() as Promise<BuildPhaseGateResponse[]>
}

export async function fetchBuildPhaseGate(phase: string): Promise<BuildPhaseGateResponse> {
  const r = await fetch(`/api/v1/build-phase/${phase}/gate`)
  if (!r.ok) throw new Error(`build phase gate: HTTP ${r.status}`)
  return r.json() as Promise<BuildPhaseGateResponse>
}

export async function runBuildPhaseGate(phase: string): Promise<RunBuildPhaseGateResponse> {
  const r = await authedFetch('build phase gate', `/api/v1/build-phase/${phase}/gate`, { method: 'POST' })
  return r.json() as Promise<RunBuildPhaseGateResponse>
}

export async function signBuildPhase(phase: string, notes = ''): Promise<RunBuildPhaseGateResponse> {
  const r = await authedFetch('build phase signoff', `/api/v1/build-phase/${phase}/signoff`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ notes }),
  })
  return r.json() as Promise<RunBuildPhaseGateResponse>
}

// Runner smoke test

