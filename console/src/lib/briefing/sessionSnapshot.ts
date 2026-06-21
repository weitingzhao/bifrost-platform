import type { AuditRecord, Reachability } from '@/api/types'
import type { BriefingSnapshotInput } from '@/lib/briefing/briefingSnapshot'

const STORAGE_KEY = 'bifrost_session_snapshot'

export interface SessionSnapshot {
  savedAt: string
  contextMetaVersion: string
  focusHeadline: string
  focusBlocker: string | null
  milestoneStatuses: Record<string, string>
  matrixFingerprints: Record<string, Record<string, Reachability>>
  cluster: {
    failingPods: number
    nodesReady: number
    nodesTotal: number
    reachability: Reachability
  }
  lastAuditAt: string | null
  trackProgress?: Record<string, Record<string, number>>
}

export function saveSnapshot(
  input: BriefingSnapshotInput,
  auditRecords: AuditRecord[],
): void {
  const milestoneStatuses: Record<string, string> = {}
  if (input.context != null) {
    for (const m of input.context.milestones) {
      milestoneStatuses[m.id] = m.status
    }
  }

  const matrixFingerprints: Record<string, Record<string, Reachability>> = {}
  for (const m of input.matrices) {
    const envFp: Record<string, Reachability> = {}
    for (const t of m.targets) {
      envFp[t.id] = t.reachability
    }
    matrixFingerprints[m.environment] = envFp
  }

  const snapshot: SessionSnapshot = {
    savedAt: new Date().toISOString(),
    contextMetaVersion: input.context?.meta.version ?? '',
    focusHeadline: input.context?.focus.headline ?? '',
    focusBlocker: input.context?.focus.blocker ?? null,
    milestoneStatuses,
    matrixFingerprints,
    cluster: {
      failingPods: input.clusterSummary?.failing_pods ?? 0,
      nodesReady: input.clusterSummary?.nodes_ready ?? 0,
      nodesTotal: input.clusterSummary?.nodes_total ?? 0,
      reachability: input.clusterSummary?.reachability ?? 'unknown',
    },
    lastAuditAt: auditRecords.length > 0 ? auditRecords[0].at : null,
    trackProgress: buildTrackProgress(input),
  }

  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(snapshot))
  } catch {
    // storage full or unavailable
  }
}

export function loadSnapshot(): SessionSnapshot | null {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw == null) return null
    return JSON.parse(raw) as SessionSnapshot
  } catch {
    return null
  }
}

export function clearSnapshot(): void {
  try {
    localStorage.removeItem(STORAGE_KEY)
  } catch {
    // ignore
  }
}

function buildTrackProgress(input: BriefingSnapshotInput): Record<string, Record<string, number>> {
  const progress: Record<string, Record<string, number>> = {}
  const tracks = input.context?.tracks
  if (tracks == null) return progress

  if (tracks.build != null) {
    const done = tracks.build.tasks.filter(t => t.status === 'done').length
    progress.build = { _done: done, _total: tracks.build.tasks.length }
  }
  if (tracks.migrate != null) {
    const streamProgress: Record<string, number> = {}
    let totalDone = 0
    let totalAll = 0
    for (const s of tracks.migrate.streams) {
      streamProgress[s.id] = s.done
      totalDone += s.done
      totalAll += s.total
    }
    streamProgress._done = totalDone
    streamProgress._total = totalAll
    progress.migrate = streamProgress
  }
  if (tracks.automate != null) {
    const streamProgress: Record<string, number> = {}
    let totalDone = 0
    let totalAll = 0
    for (const s of tracks.automate.streams) {
      streamProgress[s.id] = s.done
      totalDone += s.done
      totalAll += s.total
    }
    streamProgress._done = totalDone
    streamProgress._total = totalAll
    progress.automate = streamProgress
  }
  return progress
}
