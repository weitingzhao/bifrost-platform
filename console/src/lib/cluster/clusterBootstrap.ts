import type { ClusterNamespace } from '@/api/types'

/** Core Bifrost namespaces from clusters.yaml — used to detect bootstrap completion. */
export const CORE_BIFROST_NAMESPACES = ['cicd', 'bifrost', 'bifrost-stg', 'monitoring'] as const

export function bifrostNamespacesReady(namespaces: ClusterNamespace[] | undefined): boolean {
  if (namespaces == null || namespaces.length === 0) return false
  const names = new Set(namespaces.map(ns => ns.name))
  return CORE_BIFROST_NAMESPACES.every(name => names.has(name))
}

/** Show bootstrap shortcuts only when metrics-server or core namespaces are still missing. */
export function clusterBootstrapNeedsActions(
  metricsOk: boolean,
  namespaces: ClusterNamespace[] | undefined,
): boolean {
  return !metricsOk || !bifrostNamespacesReady(namespaces)
}
