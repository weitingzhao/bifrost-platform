import { useQuery } from '@tanstack/react-query'
import { fetchClusterNodes } from '@/api/cluster'
import { fetchPromQL, fetchTelemetryAlerts, fetchTelemetryTargets } from '@/api/telemetry'
import type { NeedsYouCount } from '@/pages/shell/needs-you/needsYouModel'
import { useNeedsYou } from '@/pages/shell/needs-you/useNeedsYou'
import { splitAlerts, yoursCount } from './ownerModel'
import {
  DAEMONSET_GAPS_QUERY,
  parseDaemonSetGaps,
  parseDownTargets,
  parseStandbyPods,
  standbyPodsQuery,
  type StandbyEvidence,
} from './standbyEvidence'

export const errorText = (error: unknown): string | null => (error == null ? null : error instanceof Error ? error.message : 'Request failed')

/** A query's answer, unless its latest read failed or is paused offline. */
export function current<T>(q: { data: T | undefined; isError: boolean; isPaused: boolean } | undefined): T | undefined {
  return q == null || q.isError || q.isPaused ? undefined : q.data
}

/**
 * What is yours: Needs you's decision items (no silent threads) plus the alerts only the Owner can
 * clear, with a standby node's alerts told apart by cluster evidence. The shell reads it for the one
 * count on For you and the app badge; the Owner Console reads it for its verdict and its lists. Both
 * read the same query keys, so this costs one set of requests however many places show it.
 */
export function useOwnerYours(now: number) {
  const needsYou = useNeedsYou()
  const alertsQ = useQuery({ queryKey: ['telemetry', 'alerts'], queryFn: fetchTelemetryAlerts, refetchInterval: 30_000, retry: false })
  const nodesQ = useQuery({ queryKey: ['cluster', 'nodes'], queryFn: fetchClusterNodes, refetchInterval: 30_000, retry: false })

  const nodes = current(nodesQ)?.nodes ?? []
  const standby = nodes.filter(n => n.elastic_mode === 'standby').map(n => ({ name: n.name, internalIp: n.internal_ip }))

  // Evidence that an alert is a standby node's (standbyEvidence.ts); read only while a node is on standby.
  const onStandby = standby.length > 0
  const podsQ = useQuery({ queryKey: ['owner', 'standby-pods', standby.map(n => n.name).join(',')], queryFn: () => fetchPromQL(standbyPodsQuery(standby)),
    enabled: onStandby, refetchInterval: 60_000, retry: false })
  const gapsQ = useQuery({ queryKey: ['owner', 'daemonset-gaps'], queryFn: () => fetchPromQL(DAEMONSET_GAPS_QUERY), enabled: onStandby, refetchInterval: 30_000, retry: false })
  const targetsQ = useQuery({ queryKey: ['telemetry', 'targets'], queryFn: () => fetchTelemetryTargets(), enabled: onStandby, refetchInterval: 30_000, retry: false })
  const pods = current(podsQ), gaps = current(gapsQ), targets = current(targetsQ)
  const evidence: StandbyEvidence | null = onStandby && pods && gaps && targets
    ? { nodes: standby, pods: parseStandbyPods(pods.points), gaps: parseDaemonSetGaps(gaps.points), down: parseDownTargets(targets.active_targets) }
    : null

  const alertsData = current(alertsQ)
  const alerts = alertsData ? splitAlerts(alertsData.alerts, evidence, now) : null
  const alertsError = alertsQ.isError ? errorText(alertsQ.error) : alertsQ.isPaused ? 'offline' : null
  const ownerAlerts: NeedsYouCount = alertsError != null ? { state: 'unknown', reason: `Alerts could not be read: ${alertsError}` }
    : alerts == null ? { state: 'loading' } : { state: 'known', count: alerts.yours.length }
  // Needs you without the silent threads: those are the coordinator's to check (Owner 2026-10-15).
  const decisions: NeedsYouCount = needsYou.count.state === 'known' ? { state: 'known', count: needsYou.decisionItems.length } : needsYou.count
  const yours = yoursCount(decisions, ownerAlerts)

  return {
    needsYou,
    alertsQ,
    nodesQ,
    nodes,
    standby,
    alerts,
    alertsError,
    standbyEvidence: onStandby ? (evidence ? 'read' as const : 'unread' as const) : 'none' as const,
    yours,
  }
}
