/**
 * Ground Fleet cell (cluster fabric).
 */
import type { ClusterSummary } from '@/api/clusterTypes'
import { PROD_ENV_FIX_SCOPE } from '@/lib/agent/prodEnvironmentFixPrompt'
import { infraSignal, type Signal } from '@/lib/control-room/missionSignals'
import { type FleetCell, type FleetStandard } from '@/lib/control-room/fleetSnapshot/types'
import { signalFromStandards, std } from '@/lib/control-room/fleetSnapshot/standards'
import { cellKey } from '@/lib/control-room/fleetSnapshot/nav'

export function buildGroundCell(input: {
  cluster?: ClusterSummary
}): FleetCell {
  const cluster = input.cluster
  const infra = infraSignal(cluster)

  const apiSig: Signal = cluster == null ? 'unknown' : (cluster.reachability as Signal)
  const nodesOk =
    cluster != null &&
    cluster.reachability === 'ok' &&
    cluster.nodes_total > 0 &&
    cluster.nodes_ready >= cluster.nodes_total
  const nodesSig: Signal =
    cluster == null ? 'unknown' : cluster.reachability === 'fail' ? 'fail' : nodesOk ? 'ok' : 'degraded'
  const podsSig: Signal =
    cluster == null ? 'unknown' : cluster.failing_pods > 0 ? 'degraded' : cluster.reachability === 'fail' ? 'fail' : 'ok'

  const standards: FleetStandard[] = [
    std(
      'cluster-api',
      'Cluster API reachable',
      apiSig,
      cluster == null ? 'Cluster: probing' : cluster.detail || cluster.reachability,
      'cluster',
    ),
    std(
      'nodes-ready',
      'All nodes Ready',
      nodesSig,
      cluster == null
        ? 'Nodes: probing'
        : `${cluster.nodes_ready}/${cluster.nodes_total} nodes Ready${
            (cluster.elastic_standby ?? 0) > 0 ? ` (+${cluster.elastic_standby} standby)` : ''
          }`,
      'cluster',
    ),
    std(
      'failing-pods',
      'No failing pods',
      podsSig,
      cluster == null
        ? 'Pods: probing'
        : cluster.failing_pods > 0
          ? `${cluster.failing_pods} failing pods`
          : 'No failing pods',
      'cluster',
    ),
  ]
  const signal = signalFromStandards(standards)
  const value =
    signal === 'ok' ? 'ready' : signal === 'fail' ? 'down' : signal === 'degraded' ? 'drift' : infra.value

  return {
    key: cellKey('ground', 'span'),
    role: 'ground',
    env: null,
    span: true,
    signal,
    value,
    detail: standards.map(s => s.reason).join(' · '),
    probePath: '',
    standards,
    fixScope: signal === 'ok' ? null : PROD_ENV_FIX_SCOPE,
    agentFixEnabled: signal !== 'ok' && signal !== 'unknown',
    agentFixDisabledReason: signal === 'unknown' ? 'Still probing' : undefined,
    escalateTabId: signal === 'fail' || signal === 'degraded' ? 'operator-plane' : undefined,
    countsTowardVerdict: true,
  }
}

