import type { TelemetryTargetEntry } from '@/api/clusterTypes'
import type { PromSamplePoint } from '@/api/telemetry'
import type { StandbyNodeRef } from '@/lib/observability/alertMapping'
import type { MappedAlert } from '@/lib/observability/types'

/**
 * Whether a firing alert is the planned standby of an elastic node (gpu-server, woken on
 * demand), proven from the cluster rather than guessed from names or percentages (review 221
 * A2, A3, B1). The evidence: the pods kube-state-metrics places on the standby nodes, each
 * DaemonSet's unavailable and mis-scheduled counts, and the scrape targets that are down. An
 * alert the evidence cannot place on a standby node stays a real alert.
 */

export type StandbyPod = { namespace: string; pod: string; ip: string; daemonset: string }
export type DaemonSetGap = { namespace: string; daemonset: string; gap: number }
export type DownTarget = { job: string; instance: string; node: string; namespace: string; pod: string }

export type StandbyEvidence = {
  nodes: StandbyNodeRef[]
  pods: StandbyPod[]
  gaps: DaemonSetGap[]
  down: DownTarget[]
}

/** PromQL for the pods on the standby nodes. Node names are matched literally. */
export function standbyPodsQuery(nodes: readonly StandbyNodeRef[]): string {
  const names = nodes.map(n => n.name.replace(/[.*+?^${}()|[\]\\]/g, '\\\\$&')).join('|')
  return `kube_pod_info{node=~"${names}"}`
}

/** Each DaemonSet's pods that are unavailable plus those on a node they should not be on (`+`, not `or`: `or` keeps one of the two). */
export const DAEMONSET_GAPS_QUERY = 'kube_daemonset_status_number_unavailable + kube_daemonset_status_number_misscheduled > 0'

export function parseStandbyPods(points: readonly PromSamplePoint[]): StandbyPod[] {
  return points.map(p => p.labels ?? {}).filter(l => l.namespace && l.pod).map(l => ({
    namespace: l.namespace, pod: l.pod, ip: l.pod_ip ?? '', daemonset: l.created_by_kind === 'DaemonSet' ? l.created_by_name ?? '' : '',
  }))
}

export function parseDaemonSetGaps(points: readonly PromSamplePoint[]): DaemonSetGap[] {
  const sum = new Map<string, DaemonSetGap>()
  for (const p of points) {
    const l = p.labels ?? {}
    if (!l.namespace || !l.daemonset || !Number.isFinite(p.value)) continue
    const key = `${l.namespace}/${l.daemonset}`
    const row = sum.get(key) ?? { namespace: l.namespace, daemonset: l.daemonset, gap: 0 }
    row.gap += p.value
    sum.set(key, row)
  }
  return [...sum.values()]
}

export function parseDownTargets(targets: readonly TelemetryTargetEntry[]): DownTarget[] {
  return targets.filter(t => t.health === 'down').map(t => ({
    job: t.labels.job ?? '', instance: t.labels.instance ?? '', node: t.labels.node ?? '', namespace: t.labels.namespace ?? '', pod: t.labels.pod ?? '',
  }))
}

const host = (instance: string) => instance.trim().toLowerCase().replace(/:\d+$/, '')

function standbyNode(ev: StandbyEvidence, node: string): boolean {
  return ev.nodes.some(n => node === n.name)
}

function standbyHost(ev: StandbyEvidence, instance: string): boolean {
  const h = host(instance)
  return h !== '' && ev.nodes.some(n => h === n.name.toLowerCase() || h === (n.internalIp ?? '').toLowerCase())
}

/** A pod is named by namespace and name; an IP never overrides a name (review 222 A-R2). */
function standbyPodNamed(ev: StandbyEvidence, namespace: string, pod: string): boolean {
  return namespace !== '' && pod !== '' && ev.pods.some(p => p.namespace === namespace && p.pod === pod)
}

/** A scrape target: by its node, else by its pod's name, else (nothing else to go on) by its address. */
function standbyTarget(ev: StandbyEvidence, t: DownTarget): boolean {
  if (t.node !== '') return standbyNode(ev, t.node)
  if (t.pod !== '') return standbyPodNamed(ev, t.namespace, t.pod)
  const h = host(t.instance)
  return standbyHost(ev, t.instance) || (h !== '' && ev.pods.some(p => p.ip !== '' && p.ip === h))
}

const NODE_ALERTS = /^(KubeNodeNotReady|KubeNodeUnreachable|KubeletInstanceUnreachable)$/
const DAEMONSET_ALERTS = /^KubeDaemonSet(RolloutStuck|MisScheduled|NotScheduled)$/

/**
 * `noise`: the evidence puts every failing part of the alert on a standby node. `likely`: a
 * DaemonSet short by no more pods than it has on standby nodes; the metrics do not say which pod
 * is short, so it is listed, not hidden (review 222 A-R1). `real`: anything else.
 */
export type StandbyVerdict = 'noise' | 'likely' | 'real'

export function standbyVerdict(alert: MappedAlert, ev: StandbyEvidence | null): StandbyVerdict {
  if (!ev || ev.nodes.length === 0) return 'real'
  const l = alert.labels
  if (NODE_ALERTS.test(alert.name)) return standbyNode(ev, l.node ?? '') || standbyHost(ev, l.instance ?? '') ? 'noise' : 'real'
  if (DAEMONSET_ALERTS.test(alert.name)) {
    const ns = l.namespace ?? '', ds = l.daemonset ?? ''
    const onStandby = ev.pods.filter(p => p.namespace === ns && p.daemonset === ds).length
    const gap = ev.gaps.find(g => g.namespace === ns && g.daemonset === ds)?.gap ?? 0
    return onStandby > 0 && gap > 0 && gap <= onStandby ? 'likely' : 'real'
  }
  if (alert.name === 'TargetDown') {
    const down = ev.down.filter(t => t.job === (l.job ?? '') && (!l.namespace || !t.namespace || t.namespace === l.namespace)
      && (!l.instance || t.instance === l.instance))
    return down.length > 0 && down.every(t => standbyTarget(ev, t)) ? 'noise' : 'real'
  }
  // A named node that is not on standby wins over anything else (review 222 A-R2).
  if (l.node) return 'real'
  // A pod-scoped alert names its pod; kube-state-metrics' own pod and instance labels are the exporter's.
  const subjectPod = alert.name.startsWith('KubePod') || l.container !== 'kube-state-metrics'
  return subjectPod && standbyPodNamed(ev, l.namespace ?? '', l.pod ?? '') ? 'noise' : 'real'
}
