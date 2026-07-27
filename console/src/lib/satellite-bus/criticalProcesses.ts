import type { Reachability } from '@/api/matrixTypes'
import type { ClusterWorkload } from '@/api/clusterTypes'

/** Platform IB Gateway plugin NS (shared bus; not per Trade env). */
export const IB_GATEWAY_PLUGIN_NS = 'data'
export const IB_GATEWAY_WORKLOAD = 'ib-gateway'

export type CriticalProcessRow = {
  label: string
  name: string
  namespace: string
  reachability: Reachability
  ready: string
  status: string
}

type CriticalProcessSpec = {
  label: string
  /** Exact workload names in the selected Trade NS (first match wins). */
  tradeNames?: readonly string[]
  /**
   * Exact names in the IB Gateway plugin NS (`data`).
   * Used when Trade NS no longer runs legacy ib-ingestor / ib-operator.
   */
  pluginNames?: readonly string[]
}

/**
 * Critical Processes catalog — exact names only (no loose regex).
 * IB Ingestor / Operator / Account Agent resolve to `data/ib-gateway` after Plugin cutover.
 */
export const CRITICAL_PROCESS_SPECS: readonly CriticalProcessSpec[] = [
  { label: 'GsTrading daemon', tradeNames: ['daemon'] },
  {
    label: 'IB Ingestor',
    tradeNames: ['ib-ingestor', 'ib-market-gateway'],
    pluginNames: [IB_GATEWAY_WORKLOAD],
  },
  {
    label: 'IB Operator',
    tradeNames: ['ib-operator', 'ib-order-gateway'],
    pluginNames: [IB_GATEWAY_WORKLOAD],
  },
  {
    label: 'IB Account Agent',
    tradeNames: ['ib-account-agent', 'ib-account-gateway'],
    pluginNames: [IB_GATEWAY_WORKLOAD],
  },
  { label: 'Massive WS', tradeNames: ['massive-ws'] },
  { label: 'Celery worker', tradeNames: ['celery-worker'] },
  { label: 'Flower', tradeNames: ['flower'] },
]

function findByExactName(
  workloads: ClusterWorkload[],
  names: readonly string[] | undefined,
): ClusterWorkload | undefined {
  if (names == null || names.length === 0) return undefined
  for (const name of names) {
    const match = workloads.find(w => w.name === name)
    if (match != null) return match
  }
  return undefined
}

function toRow(
  label: string,
  match: ClusterWorkload | undefined,
  fallbackNs: string,
  viaPlugin: boolean,
): CriticalProcessRow {
  if (match == null) {
    return {
      label,
      name: '—',
      namespace: fallbackNs,
      reachability: 'unknown',
      ready: '—',
      status: 'not deployed',
    }
  }
  const status =
    viaPlugin && !match.status.toLowerCase().includes('plugin')
      ? `${match.status} · IB Gateway plugin`
      : match.status
  return {
    label,
    name: match.name,
    namespace: match.namespace,
    reachability: match.reachability,
    ready: match.ready,
    status,
  }
}

/**
 * Resolve Critical Processes for Operate · Evidence.
 * Prefer Trade-NS exact names; fall back to `data/ib-gateway` for IB edge rows.
 */
export function resolveCriticalProcesses(
  tradeNs: string,
  tradeWorkloads: ClusterWorkload[],
  pluginWorkloads: ClusterWorkload[],
  pluginNs: string = IB_GATEWAY_PLUGIN_NS,
): CriticalProcessRow[] {
  return CRITICAL_PROCESS_SPECS.map(spec => {
    const tradeMatch = findByExactName(tradeWorkloads, spec.tradeNames)
    if (tradeMatch != null) {
      return toRow(spec.label, tradeMatch, tradeNs, false)
    }
    const pluginMatch = findByExactName(pluginWorkloads, spec.pluginNames)
    if (pluginMatch != null) {
      return toRow(spec.label, pluginMatch, pluginNs, true)
    }
    return toRow(spec.label, undefined, tradeNs, false)
  })
}
