import type { BriefingSessionPackResponse, BriefingSessionResultsResponse, CloseBriefingSessionRequest, CloseBriefingSessionResponse } from './agentTypes'
import { authedFetch } from './client'

export async function fetchBriefingSessionPack(params?: {
  track?: string
  lane?: string
  intent?: string
  pack?: string
}): Promise<BriefingSessionPackResponse> {
  const qs = new URLSearchParams()
  if (params?.track != null) qs.set('track', params.track)
  if (params?.lane != null) qs.set('lane', params.lane)
  if (params?.intent != null) qs.set('intent', params.intent)
  if (params?.pack != null) qs.set('pack', params.pack)
  const suffix = qs.toString() !== '' ? `?${qs.toString()}` : ''
  const r = await fetch(`/api/v1/briefing/session-pack${suffix}`)
  if (!r.ok) throw new Error(`briefing/session-pack: HTTP ${r.status}`)
  return r.json() as Promise<BriefingSessionPackResponse>
}

export async function fetchBriefingSessionResults(): Promise<BriefingSessionResultsResponse> {
  const r = await fetch('/api/v1/briefing/session-results')
  if (!r.ok) throw new Error(`briefing/session-results: HTTP ${r.status}`)
  return r.json() as Promise<BriefingSessionResultsResponse>
}

export async function closeBriefingSession(
  body: CloseBriefingSessionRequest,
): Promise<CloseBriefingSessionResponse> {
  const r = await authedFetch('briefing/session-results', '/api/v1/briefing/session-results', {
    method: 'POST',
    body: JSON.stringify(body),
  })
  return r.json() as Promise<CloseBriefingSessionResponse>
}

