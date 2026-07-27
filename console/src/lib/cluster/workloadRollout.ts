import type { ClusterWorkload } from '@/api/clusterTypes'

/** True when Deployment reports a full rollout (updated + ready + available). */
export function isDeploymentRolloutComplete(w: ClusterWorkload | null | undefined): boolean {
  if (w == null) return false
  if (!w.kind.toLowerCase().includes('deploy')) return false
  const desired = w.desired_replicas
  if (desired == null) {
    // Legacy API: ready "n/n" and status Ready.
    if (w.status === 'Unavailable') return false
    const parts = w.ready.split('/')
    if (parts.length !== 2) return false
    const ready = Number(parts[0])
    const want = Number(parts[1])
    return Number.isFinite(ready) && Number.isFinite(want) && ready === want && w.status === 'Ready'
  }
  if (desired === 0) return true
  const updated = w.updated_replicas ?? 0
  const ready = w.ready_replicas ?? 0
  const available = w.available_replicas ?? 0
  return (
    updated >= desired &&
    ready >= desired &&
    available >= desired &&
    w.status !== 'Unavailable' &&
    w.status !== 'Progressing'
  )
}

/** Compact operator-facing rollout line, e.g. "upd 1/2 · ready 2/2 · Progressing". */
export function formatWorkloadRollout(w: ClusterWorkload | null | undefined): string | null {
  if (w == null) return null
  if (!w.kind.toLowerCase().includes('deploy')) return null
  const desired = w.desired_replicas
  if (desired == null) {
    if (w.status && w.status !== 'Ready') return w.status
    return null
  }
  const updated = w.updated_replicas ?? 0
  const ready = w.ready_replicas ?? 0
  const available = w.available_replicas ?? 0
  const parts = [
    `upd ${updated}/${desired}`,
    `ready ${ready}/${desired}`,
    `avail ${available}/${desired}`,
  ]
  if (w.status && w.status !== 'Ready') {
    parts.push(w.status)
  }
  return parts.join(' · ')
}
