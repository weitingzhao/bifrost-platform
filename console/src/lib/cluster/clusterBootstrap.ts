import type { ClusterNamespace } from '@/api/clusterTypes'

/** Core Bifrost namespaces from clusters.yaml — used to detect bootstrap completion. */
export const CORE_BIFROST_NAMESPACES = ['cicd', 'bifrost-stg', 'bifrost-platform-stg', 'monitoring'] as const

/**
 * Whether every CORE_BIFROST_NAMESPACES entry exists in the cluster list.
 * Callers must pass the **full** namespace inventory (not a bifrost*-filtered subset):
 * core set includes `cicd` and `monitoring`.
 */
export function bifrostNamespacesReady(namespaces: ClusterNamespace[] | undefined): boolean {
  if (namespaces == null || namespaces.length === 0) return false
  const names = new Set(namespaces.map(ns => ns.name))
  return CORE_BIFROST_NAMESPACES.every(name => names.has(name))
}

/** Names from CORE_BIFROST_NAMESPACES still missing (for evidence / copy). */
export function missingCoreBifrostNamespaces(
  namespaces: ClusterNamespace[] | undefined,
): string[] {
  if (namespaces == null) return [...CORE_BIFROST_NAMESPACES]
  const names = new Set(namespaces.map(ns => ns.name))
  return CORE_BIFROST_NAMESPACES.filter(name => !names.has(name))
}

/**
 * Show bootstrap shortcuts only when metrics-server or core namespaces are still missing.
 * `namespaces === undefined` means the list has not loaded yet — do not treat as incomplete.
 */
export function clusterBootstrapNeedsActions(
  metricsOk: boolean,
  namespaces: ClusterNamespace[] | undefined,
): boolean {
  if (!metricsOk) return true
  if (namespaces == null) return false
  return !bifrostNamespacesReady(namespaces)
}
