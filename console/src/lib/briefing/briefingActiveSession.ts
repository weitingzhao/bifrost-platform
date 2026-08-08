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
  /** Delivery Board program bound to this session — shared with Build TCC. */
  programId?: string
}

export function saveBriefingActiveSession(session: BriefingActiveSession): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(session))
    window.dispatchEvent(new Event('bifrost-briefing-active-session'))
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

/** Bind a Delivery program to the current Active Session (same lane context). */
export function attachProgramToBriefingSession(programId: string): void {
  const current = loadBriefingActiveSession()
  if (current == null) return
  const id = programId.trim()
  if (id === '') return
  if (current.programId === id) return
  saveBriefingActiveSession({ ...current, programId: id })
}

/** Drop program bind when lane mismatches or instance is discarded. */
export function clearProgramFromBriefingSession(): void {
  const current = loadBriefingActiveSession()
  if (current == null || current.programId == null) return
  saveBriefingActiveSession({ ...current, programId: undefined })
}

export function clearBriefingActiveSession(): void {
  try {
    localStorage.removeItem(STORAGE_KEY)
    window.dispatchEvent(new Event('bifrost-briefing-active-session'))
  } catch {
    // ignore
  }
}
