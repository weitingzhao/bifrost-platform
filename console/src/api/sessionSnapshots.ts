import type { SessionSnapshotLatestResponse, SessionSnapshotSaveResponse } from './agentTypes'
import { authedFetch } from './client'
import type { SessionSnapshot } from '@/lib/briefing/sessionSnapshot'

export async function fetchSessionSnapshotLatest(): Promise<SessionSnapshotLatestResponse> {
  const r = await fetch('/api/v1/session-snapshots/latest')
  if (!r.ok) throw new Error(`session-snapshots/latest: HTTP ${r.status}`)
  return r.json() as Promise<SessionSnapshotLatestResponse>
}

export async function saveSessionSnapshot(snapshot: SessionSnapshot): Promise<SessionSnapshotSaveResponse> {
  const r = await authedFetch('session-snapshots', '/api/v1/session-snapshots', {
    method: 'POST',
    body: JSON.stringify(snapshot),
  })
  return r.json() as Promise<SessionSnapshotSaveResponse>
}

