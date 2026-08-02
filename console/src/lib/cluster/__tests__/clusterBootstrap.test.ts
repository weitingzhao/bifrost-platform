import { describe, expect, it } from 'vitest'
import type { ClusterNamespace } from '@/api/clusterTypes'
import {
  bifrostNamespacesReady,
  clusterBootstrapNeedsActions,
  CORE_BIFROST_NAMESPACES,
  missingCoreBifrostNamespaces,
} from '@/lib/cluster/clusterBootstrap'

function ns(name: string): ClusterNamespace {
  return { name, status: 'Active', pod_count: 0, running_pods: 0, failing_pods: 0 }
}

describe('bifrostNamespacesReady', () => {
  it('is true when full inventory includes every CORE namespace', () => {
    const all = [
      ...CORE_BIFROST_NAMESPACES.map(ns),
      ns('bifrost-dev'),
      ns('bifrost-prod'),
      ns('kube-system'),
    ]
    expect(bifrostNamespacesReady(all)).toBe(true)
    expect(missingCoreBifrostNamespaces(all)).toEqual([])
  })

  it('is false when cicd/monitoring are stripped by a bifrost* filter', () => {
    const bifrostOnly = [ns('bifrost-stg'), ns('bifrost-platform-stg'), ns('bifrost-dev')]
    expect(bifrostNamespacesReady(bifrostOnly)).toBe(false)
    expect(missingCoreBifrostNamespaces(bifrostOnly)).toEqual(['cicd', 'monitoring'])
  })

  it('is false when a single core namespace is missing', () => {
    const missingMonitoring = CORE_BIFROST_NAMESPACES.filter(n => n !== 'monitoring').map(ns)
    expect(bifrostNamespacesReady(missingMonitoring)).toBe(false)
    expect(missingCoreBifrostNamespaces(missingMonitoring)).toEqual(['monitoring'])
  })
})

describe('clusterBootstrapNeedsActions', () => {
  const complete = CORE_BIFROST_NAMESPACES.map(ns)

  it('false when metrics ok and core namespaces present', () => {
    expect(clusterBootstrapNeedsActions(true, complete)).toBe(false)
  })

  it('true when metrics-server missing', () => {
    expect(clusterBootstrapNeedsActions(false, complete)).toBe(true)
  })

  it('true when a core namespace is missing', () => {
    expect(clusterBootstrapNeedsActions(true, [ns('bifrost-stg'), ns('cicd')])).toBe(true)
  })

  it('does not false-positive while namespace list is still loading', () => {
    expect(clusterBootstrapNeedsActions(true, undefined)).toBe(false)
  })

  it('regression: bifrost*-only list must not be used for completion (would always need actions)', () => {
    const bifrostOnly = complete.filter(n => n.name.startsWith('bifrost'))
    // Old buggy caller passed this list → always true. Document expected ready=false on subset.
    expect(bifrostNamespacesReady(bifrostOnly)).toBe(false)
    expect(clusterBootstrapNeedsActions(true, bifrostOnly)).toBe(true)
    // Correct caller passes full inventory:
    expect(clusterBootstrapNeedsActions(true, complete)).toBe(false)
  })
})
