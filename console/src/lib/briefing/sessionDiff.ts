import type { AuditRecord, Reachability } from '@/api/types'
import type { BriefingSnapshotInput } from '@/lib/briefing/briefingSnapshot'
import type { SessionSnapshot } from '@/lib/briefing/sessionSnapshot'

export interface MilestoneChange {
  id: string
  from: string
  to: string
}

export interface MatrixChange {
  env: string
  targetId: string
  from: Reachability
  to: Reachability
}

export interface ClusterChanges {
  failingPodsDelta: number
  nodesReadyDelta: number
  nodesReadyFrom: number
  nodesReadyTo: number
  reachabilityChanged: boolean
  reachabilityFrom: Reachability
  reachabilityTo: Reachability
}

export interface TrackChange {
  trackId: string
  label: string
  doneFrom: number
  doneTo: number
  total: number
}

export interface SessionDelta {
  timeSince: string
  savedAt: string
  spineVersionChanged: boolean
  spineVersionFrom: string
  spineVersionTo: string
  focusChanged: { from: string; to: string } | null
  blockerChanged: { from: string | null; to: string | null } | null
  milestoneChanges: MilestoneChange[]
  matrixChanges: MatrixChange[]
  clusterChanges: ClusterChanges | null
  newAuditRecords: AuditRecord[]
  trackChanges: TrackChange[]
}

function formatTimeSince(isoDate: string): string {
  const diff = Date.now() - new Date(isoDate).getTime()
  const mins = Math.floor(diff / 60_000)
  if (mins < 1) return 'just now'
  if (mins < 60) return `${mins}m ago`
  const hrs = Math.floor(mins / 60)
  if (hrs < 24) return `${hrs}h ago`
  const days = Math.floor(hrs / 24)
  if (days === 1) return 'yesterday'
  return `${days}d ago`
}

export function computeSessionDelta(
  prev: SessionSnapshot,
  current: BriefingSnapshotInput,
  auditRecords: AuditRecord[],
): SessionDelta {
  const currentVersion = current.context?.meta.version ?? ''
  const spineVersionChanged = prev.contextMetaVersion !== '' && prev.contextMetaVersion !== currentVersion

  const currentHeadline = current.context?.focus.headline ?? ''
  const focusChanged =
    prev.focusHeadline !== '' && prev.focusHeadline !== currentHeadline
      ? { from: prev.focusHeadline, to: currentHeadline }
      : null

  const currentBlocker = current.context?.focus.blocker ?? null
  const blockerChanged =
    prev.focusBlocker !== currentBlocker
      ? { from: prev.focusBlocker, to: currentBlocker }
      : null

  const milestoneChanges: MilestoneChange[] = []
  if (current.context != null) {
    for (const m of current.context.milestones) {
      const prevStatus = prev.milestoneStatuses[m.id]
      if (prevStatus != null && prevStatus !== m.status) {
        milestoneChanges.push({ id: m.id, from: prevStatus, to: m.status })
      }
    }
  }

  const matrixChanges: MatrixChange[] = []
  for (const m of current.matrices) {
    const prevEnv = prev.matrixFingerprints[m.environment]
    if (prevEnv == null) continue
    for (const t of m.targets) {
      const prevReach = prevEnv[t.id]
      if (prevReach != null && prevReach !== t.reachability) {
        matrixChanges.push({ env: m.environment, targetId: t.id, from: prevReach, to: t.reachability })
      }
    }
  }

  let clusterChanges: ClusterChanges | null = null
  if (current.clusterSummary != null) {
    const fpDelta = current.clusterSummary.failing_pods - prev.cluster.failingPods
    const nrDelta = current.clusterSummary.nodes_ready - prev.cluster.nodesReady
    const reachChanged = prev.cluster.reachability !== current.clusterSummary.reachability
    if (fpDelta !== 0 || nrDelta !== 0 || reachChanged) {
      clusterChanges = {
        failingPodsDelta: fpDelta,
        nodesReadyDelta: nrDelta,
        nodesReadyFrom: prev.cluster.nodesReady,
        nodesReadyTo: current.clusterSummary.nodes_ready,
        reachabilityChanged: reachChanged,
        reachabilityFrom: prev.cluster.reachability,
        reachabilityTo: current.clusterSummary.reachability,
      }
    }
  }

  const newAuditRecords = prev.lastAuditAt != null
    ? auditRecords.filter(r => r.at > prev.lastAuditAt!)
    : auditRecords

  const trackChanges: TrackChange[] = []
  const prevTp = prev.trackProgress ?? {}
  const tracks = current.context?.tracks
  if (tracks != null) {
    if (tracks.build != null) {
      const curDone = tracks.build.tasks.filter(t => t.status === 'done').length
      const prevDone = prevTp.build?._done ?? 0
      if (curDone !== prevDone) {
        trackChanges.push({ trackId: 'build', label: 'Build', doneFrom: prevDone, doneTo: curDone, total: tracks.build.tasks.length })
      }
    }
    if (tracks.migrate != null) {
      let curDone = 0
      let curTotal = 0
      for (const s of tracks.migrate.streams) { curDone += s.done; curTotal += s.total }
      const prevDone = prevTp.migrate?._done ?? 0
      if (curDone !== prevDone) {
        trackChanges.push({ trackId: 'migrate', label: 'Migrate', doneFrom: prevDone, doneTo: curDone, total: curTotal })
      }
    }
    if (tracks.automate != null) {
      let curDone = 0
      let curTotal = 0
      for (const s of tracks.automate.streams) { curDone += s.done; curTotal += s.total }
      const prevDone = prevTp.automate?._done ?? 0
      if (curDone !== prevDone) {
        trackChanges.push({ trackId: 'automate', label: 'Automate', doneFrom: prevDone, doneTo: curDone, total: curTotal })
      }
    }
  }

  return {
    timeSince: formatTimeSince(prev.savedAt),
    savedAt: prev.savedAt,
    spineVersionChanged,
    spineVersionFrom: prev.contextMetaVersion,
    spineVersionTo: currentVersion,
    focusChanged,
    blockerChanged,
    milestoneChanges,
    matrixChanges,
    clusterChanges,
    newAuditRecords,
    trackChanges,
  }
}

export function isEmptyDelta(delta: SessionDelta): boolean {
  return (
    !delta.spineVersionChanged &&
    delta.focusChanged == null &&
    delta.blockerChanged == null &&
    delta.milestoneChanges.length === 0 &&
    delta.matrixChanges.length === 0 &&
    delta.clusterChanges == null &&
    delta.newAuditRecords.length === 0 &&
    delta.trackChanges.length === 0
  )
}

export function formatDeltaForPack(delta: SessionDelta): string {
  if (isEmptyDelta(delta)) {
    return '## Since last session\n\nNo significant changes since last session.'
  }

  const lines = ['## Since last session', '', `Last snapshot: ${delta.savedAt} (${delta.timeSince})`, '']

  if (delta.spineVersionChanged) {
    lines.push(`- Spine version: ${delta.spineVersionFrom} -> ${delta.spineVersionTo}`)
  }
  if (delta.focusChanged != null) {
    lines.push(`- Focus headline changed: "${delta.focusChanged.from}" -> "${delta.focusChanged.to}"`)
  }
  if (delta.blockerChanged != null) {
    lines.push(`- Blocker: ${delta.blockerChanged.from ?? '(none)'} -> ${delta.blockerChanged.to ?? '(none)'}`)
  }
  for (const mc of delta.milestoneChanges) {
    lines.push(`- Milestone ${mc.id}: ${mc.from} -> ${mc.to}`)
  }
  for (const mx of delta.matrixChanges) {
    lines.push(`- Matrix [${mx.env}] ${mx.targetId}: ${mx.from} -> ${mx.to}`)
  }
  if (delta.clusterChanges != null) {
    const cc = delta.clusterChanges
    if (cc.reachabilityChanged) {
      lines.push(`- Cluster reachability: ${cc.reachabilityFrom} -> ${cc.reachabilityTo}`)
    }
    if (cc.failingPodsDelta !== 0) {
      lines.push(`- Failing pods: ${cc.failingPodsDelta > 0 ? '+' : ''}${cc.failingPodsDelta}`)
    }
    if (cc.nodesReadyDelta !== 0) {
      lines.push(`- Nodes ready: ${cc.nodesReadyFrom} -> ${cc.nodesReadyTo}`)
    }
  }
  if (delta.newAuditRecords.length > 0) {
    lines.push(`- New actuation records: ${delta.newAuditRecords.length}`)
    for (const r of delta.newAuditRecords.slice(0, 5)) {
      lines.push(`  - [${r.at}] ${r.action} ${r.target} (${r.status})`)
    }
    if (delta.newAuditRecords.length > 5) {
      lines.push(`  - ... and ${delta.newAuditRecords.length - 5} more`)
    }
  }
  for (const tc of delta.trackChanges) {
    lines.push(`- Track ${tc.label}: ${tc.doneFrom}→${tc.doneTo}/${tc.total} items done`)
  }

  return lines.join('\n')
}
