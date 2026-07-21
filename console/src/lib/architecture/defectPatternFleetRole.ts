/**
 * Map Defects retrospective patterns → Fleet Desk Role (role×env board).
 *
 * System Domain (Apollo sidebar) ≠ Fleet Role. Mission Control can run a check
 * that targets Satellite STG — Domain stays Mission Control; Role is Satellite.
 */
import type { FleetRole } from '@/lib/control-room/fleetSnapshot'
import {
  patternDominantScope,
  patternToDomain,
  type SystemDomainId,
} from '@/lib/architecture/systemDomainCatalog'

function extractTargetNamespace(pattern: {
  label: string
  component?: { namespace?: string }
}): string {
  const fromComponent = pattern.component?.namespace?.trim() ?? ''
  if (fromComponent !== '' && fromComponent !== '_global') return fromComponent
  const label = pattern.label ?? ''
  if (label.includes('→')) {
    const right = label.split('→').slice(1).join('→').trim()
    if (right !== '' && right !== '_global') return right
  }
  return ''
}

/** Namespace / target → Fleet Role (preferred when present). */
export function namespaceToFleetRole(namespace: string): FleetRole | null {
  const n = namespace.trim().toLowerCase()
  if (n === '') return null

  if (
    n.startsWith('bifrost-platform') ||
    n === 'cicd' ||
    n.includes('tekton') ||
    n === 'argocd' ||
    n.includes('argo')
  ) {
    return 'rocket'
  }
  if (n === 'bifrost-dev' || n === 'bifrost-stg' || n === 'bifrost-prod') {
    return 'satellite'
  }
  if (n.includes('ib') || n.includes('plugin') || n.includes('vendor')) {
    return 'vendor'
  }
  if (
    n === 'data' ||
    n.includes('cnpg') ||
    n.includes('redis') ||
    n.includes('postgres') ||
    n === 'kube-system'
  ) {
    return 'ground'
  }
  return null
}

function scopeHintToFleetRole(scope: string): FleetRole | null {
  const key = scope.trim().toLowerCase()
  if (key === '') return null
  if (
    key.includes('cluster') ||
    key.includes('release') ||
    key.includes('gitops') ||
    key.includes('pipeline') ||
    key.includes('cicd') ||
    key.includes('registry') ||
    key.includes('platform-self')
  ) {
    return 'rocket'
  }
  if (key.includes('trade') || key.includes('satellite') || key.includes('deliver')) {
    return 'satellite'
  }
  if (key.includes('ib') || key.includes('plugin') || key.includes('vendor')) {
    return 'vendor'
  }
  if (key.includes('network') || key.includes('unifi') || key.includes('vlan')) {
    return 'ground'
  }
  if (
    key.includes('agent-desk') ||
    key.includes('operator-plane') ||
    key.includes('git-dirty') ||
    key === 'ops'
  ) {
    return 'engineer'
  }
  return null
}

function domainToFleetRoleFallback(domain: SystemDomainId): FleetRole {
  switch (domain) {
    case 'rocket':
      return 'rocket'
    case 'satellite':
      return 'satellite'
    case 'ground-systems':
      return 'ground'
    case 'subcontractors':
      return 'vendor'
    case 'engineer':
    case 'governance':
    case 'mission-control':
      return 'engineer'
  }
}

/**
 * Fleet Role for a pattern: target namespace → scope hints → System Domain fallback.
 */
export function patternToFleetRole(pattern: {
  label: string
  component?: { namespace?: string }
  jobs?: { scope?: string }[]
}): FleetRole {
  const ns = extractTargetNamespace(pattern)
  const fromNs = namespaceToFleetRole(ns)
  if (fromNs != null) return fromNs

  const scope = patternDominantScope(pattern)
  const fromScope = scopeHintToFleetRole(scope)
  if (fromScope != null) return fromScope

  return domainToFleetRoleFallback(patternToDomain(pattern))
}
