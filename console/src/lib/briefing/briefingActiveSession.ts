import type { TrackId } from '@/lib/briefing/workTracks'
import type { LaneId } from '@/lib/briefing/workLanes'
import type { WorkIntent } from '@/lib/briefing/workIntents'
import type { BriefingPackSize } from '@/lib/briefing/briefingUrlState'

const STORAGE_KEY = 'bifrost_briefing_active_session'

export interface BriefingActiveSession {
  track: TrackId
  lane: LaneId
  intent: WorkIntent
  packSize: BriefingPackSize
  startedAt: string
  jobId?: string
}

export function saveBriefingActiveSession(session: BriefingActiveSession): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(session))
  } catch {
    // ignore
  }
}

export function loadBriefingActiveSession(): BriefingActiveSession | null {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw == null) return null
    return JSON.parse(raw) as BriefingActiveSession
  } catch {
    return null
  }
}

export function attachJobToBriefingSession(jobId: string): void {
  const current = loadBriefingActiveSession()
  if (current == null) return
  saveBriefingActiveSession({ ...current, jobId })
}

export function clearBriefingActiveSession(): void {
  try {
    localStorage.removeItem(STORAGE_KEY)
  } catch {
    // ignore
  }
}
