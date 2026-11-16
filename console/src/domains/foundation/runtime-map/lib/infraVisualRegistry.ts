import { tenantIcons, isTenantCustomIcon, tenantComponents, tenantScopeComponents, tenantRoleRules, isTenantApiTarget, tenantMatrixTarget } from '@/domains/tenant/runtime-map/visualAdapter'
import type { SimpleIcon } from 'simple-icons'
import {
  siArgo,
  siDocker,
  siGitea,
  siGo,
  siGrafana,
  siK3s,
  siNginx,
  siOllama,
  siPostgresql,
  siPrometheus,
  siRedis,
  siTekton,
  siTraefikproxy,
} from 'simple-icons'

import type { Reachability, TopologyMatrixService, TopologyNode, TopologyResponse } from '@/api/matrixTypes'
import type { ScopeTag } from '@/domains/foundation/runtime-map/lib/runtimeMapRegistry'

import type { RoleView, InfraComponentVisual, StackChipModel } from '../contracts'
export type { RoleView, InfraComponentVisual, StackChipModel } from '../contracts'

const ICON_BY_COMPONENT: Record<string, SimpleIcon | undefined> = {
  ...tenantIcons,
  postgres: siPostgresql,
  redis: siRedis,
  nginx: siNginx,
  docker: siDocker,
  k3s: siK3s,
  traefik: siTraefikproxy,
  gitea: siGitea,
  argocd: siArgo,
  tekton: siTekton,
  registry: siDocker,
  prometheus: siPrometheus,
  grafana: siGrafana,
  platform: siGo,
  ollama: siOllama,
}

const INFRA_COMPONENTS: Record<string, InfraComponentVisual> = {
  ...tenantComponents,
  postgres: {
    id: 'postgres',
    label: 'PostgreSQL',
    lettermark: 'PG',
    brandColor: '#336791',
    simpleIconSlug: 'postgresql',
    scopeTag: 'PG',
    matrixTargets: ['postgres'],
  },
  redis: {
    id: 'redis',
    label: 'Redis',
    lettermark: 'RD',
    brandColor: '#DC382D',
    simpleIconSlug: 'redis',
    scopeTag: 'REDIS',
    matrixTargets: ['redis'],
  },
  nginx: {
    id: 'nginx',
    label: 'nginx',
    lettermark: 'NX',
    brandColor: '#009639',
    simpleIconSlug: 'nginx',
    scopeTag: 'INFRA',
    matrixTargets: ['nginx-spa'],
  },
  docker: {
    id: 'docker',
    label: 'Docker Compose',
    lettermark: 'DC',
    brandColor: '#2496ED',
    simpleIconSlug: 'docker',
    scopeTag: 'INFRA',
  },
  k3s: {
    id: 'k3s',
    label: 'K3s',
    lettermark: 'K3',
    brandColor: '#FFC117',
    simpleIconSlug: 'k3s',
    scopeTag: 'K3S',
    plannedInCompose: true,
  },
  traefik: {
    id: 'traefik',
    label: 'Traefik',
    lettermark: 'TF',
    brandColor: '#24A1C1',
    simpleIconSlug: 'traefikproxy',
    scopeTag: 'K3S',
    plannedInCompose: true,
  },
  gitea: {
    id: 'gitea',
    label: 'Gitea',
    lettermark: 'GT',
    brandColor: '#609926',
    simpleIconSlug: 'gitea',
    scopeTag: 'GITHUB',
    plannedInCompose: true,
  },
  argocd: {
    id: 'argocd',
    label: 'ArgoCD',
    lettermark: 'AR',
    brandColor: '#EF7B4D',
    simpleIconSlug: 'argo',
    scopeTag: 'GITHUB',
    plannedInCompose: true,
  },
  tekton: {
    id: 'tekton',
    label: 'Tekton',
    lettermark: 'TK',
    brandColor: '#FD495C',
    simpleIconSlug: 'tekton',
    scopeTag: 'GITHUB',
    plannedInCompose: true,
  },
  registry: {
    id: 'registry',
    label: 'Container Registry',
    lettermark: 'RG',
    brandColor: '#2496ED',
    simpleIconSlug: 'docker',
    scopeTag: 'GITHUB',
    plannedInCompose: true,
  },
  prometheus: {
    id: 'prometheus',
    label: 'Prometheus',
    lettermark: 'PR',
    brandColor: '#E6522C',
    simpleIconSlug: 'prometheus',
    scopeTag: 'OBSERVE',
    plannedInCompose: true,
  },
  grafana: {
    id: 'grafana',
    label: 'Grafana',
    lettermark: 'GR',
    brandColor: '#F46800',
    simpleIconSlug: 'grafana',
    scopeTag: 'OBSERVE',
    plannedInCompose: true,
  },
  platform: {
    id: 'platform',
    label: 'Platform API',
    lettermark: 'GO',
    brandColor: '#00ADD8',
    simpleIconSlug: 'go',
    scopeTag: 'PLATFORM',
    // The platform's own token probe into Trade. Not api-ops: that is a Trade gateway prefix
    // answered by the api-monitor process, so it belongs to Trade APIs (TD-55).
    matrixTargets: ['ops-capabilities'],
  },
  ollama: {
    id: 'ollama',
    label: 'Ollama',
    lettermark: 'OL',
    brandColor: '#1a1a1a',
    simpleIconSlug: 'ollama',
    scopeTag: 'AI',
    plannedInCompose: true,
  },
  generic: {
    id: 'generic',
    label: 'Service',
    lettermark: '?',
    brandColor: '#6b7280',
  },
}

/** Default infra component icon per SCOPE tag (right panel group headers). */
const SCOPE_DEFAULT_COMPONENT: Partial<Record<ScopeTag, string>> = {
  ...tenantScopeComponents,
  PLATFORM: 'platform',
  INFRA: 'nginx',
  PG: 'postgres',
  REDIS: 'redis',
  K3S: 'k3s',
  GITHUB: 'gitea',
  OBSERVE: 'prometheus',
  AI: 'ollama',
}

const ROLE_MAP: Array<{ match: (role: string) => boolean; componentId: string }> = [
  { match: r => r.includes('postgres') || r === 'cnpg', componentId: 'postgres' },
  { match: r => r === 'redis', componentId: 'redis' },
  { match: r => r === 'nginx' || r === 'ingress', componentId: 'nginx' },
  { match: r => r.includes('compose') || r === 'prod_compose' || r === 'dev_compose', componentId: 'docker' },
  { match: r => r.startsWith('k3s'), componentId: 'k3s' },
  tenantRoleRules.api,
  { match: r => r === 'gitea', componentId: 'gitea' },
  { match: r => r === 'argocd', componentId: 'argocd' },
  { match: r => r === 'tekton', componentId: 'tekton' },
  { match: r => r === 'monitoring', componentId: 'prometheus' },
  tenantRoleRules.ib,
  { match: r => r === 'ollama', componentId: 'ollama' },
  tenantRoleRules.socket,
  tenantRoleRules.frontend,
  { match: r => r === 'platform_console' || r === 'kubectl' || r === 'mcp_client', componentId: 'platform' },
  { match: r => r === 'ci' || r === 'ci_runner' || r === 'watchdog', componentId: 'tekton' },
  { match: r => r === 'planned_staging', componentId: 'k3s' },
  { match: r => r === 'dev_workstation', componentId: 'docker' },
]

function roleToComponentId(role: string): string {
  for (const rule of ROLE_MAP) {
    if (rule.match(role)) return rule.componentId
  }
  return 'generic'
}

export function getComponentVisual(componentId: string): InfraComponentVisual {
  return INFRA_COMPONENTS[componentId] ?? INFRA_COMPONENTS.generic
}

export function getIconPath(componentId: string): string | undefined {
  if (isTenantCustomIcon(componentId)) return undefined
  return ICON_BY_COMPONENT[componentId]?.path
}

export function componentIdForScopeTag(tag: ScopeTag): string {
  return SCOPE_DEFAULT_COMPONENT[tag] ?? 'generic'
}

function reachForComponent(
  component: InfraComponentVisual,
  services: TopologyMatrixService[],
): Reachability {
  const targets = component.matrixTargets ?? []
  if (targets.length === 0) {
    const prefixMatch = services.filter(
      s =>
        isTenantApiTarget(component.id, s.id) ||
        (component.id === 'platform' && s.id === 'ops-capabilities'),
    )
    if (prefixMatch.length === 0) return 'unknown'
    if (prefixMatch.some(s => s.reachability === 'fail')) return 'fail'
    if (prefixMatch.some(s => s.reachability === 'degraded')) return 'degraded'
    if (prefixMatch.every(s => s.reachability === 'ok')) return 'ok'
    return 'unknown'
  }

  const matched = services.filter(s => targets.includes(s.id))
  if (matched.length === 0) return 'unknown'
  if (matched.some(s => s.reachability === 'fail')) return 'fail'
  if (matched.some(s => s.reachability === 'degraded')) return 'degraded'
  if (matched.every(s => s.reachability === 'ok')) return 'ok'
  return 'unknown'
}

function matrixTargetForChip(
  component: InfraComponentVisual,
  services: TopologyMatrixService[],
  explicitTarget?: string,
): string | undefined {
  if (explicitTarget) return explicitTarget
  if (component.matrixTargets?.length === 1) return component.matrixTargets[0]
  const tenantTarget = tenantMatrixTarget(component.id, services)
  if (tenantTarget != null) return tenantTarget
  if (component.id === 'platform') return 'ops-capabilities'
  return undefined
}

function buildChip(
  role: string,
  roleView: RoleView,
  services: TopologyMatrixService[],
  ghost: boolean,
  matrixTarget?: string,
): StackChipModel {
  const componentId = roleToComponentId(role)
  const component = getComponentVisual(componentId)
  const reach = reachForComponent(component, services)
  const planned =
    ghost ||
    (roleView === 'k3s' &&
      component.plannedInCompose === true &&
      reach === 'unknown') ||
    (roleView === 'compose' && component.plannedInK3s === true) ||
    role === 'planned_staging'

  return {
    chipId: `${role}-${componentId}${ghost ? '-ghost' : ''}`,
    componentId: component.id,
    label: component.label,
    lettermark: component.lettermark,
    brandColor: component.brandColor,
    planned,
    ghost,
    reachability: planned && reach === 'unknown' ? 'unknown' : reach,
    matrixTargetId: matrixTargetForChip(component, services, matrixTarget),
    scopeTag: component.scopeTag,
  }
}

const REACH_RANK: Record<Reachability, number> = {
  fail: 0,
  degraded: 1,
  ok: 2,
  unknown: 3,
}

/** Collapse roles that map to the same visual component (e.g. tws + ib_host → ib). */
function mergeChipsByComponent(chips: StackChipModel[]): StackChipModel[] {
  const order: StackChipModel[] = []
  const indexByComponent = new Map<string, number>()

  for (const chip of chips) {
    const idx = indexByComponent.get(chip.componentId)
    if (idx === undefined) {
      indexByComponent.set(chip.componentId, order.length)
      order.push(chip)
      continue
    }

    const existing = order[idx]
    if (existing.ghost && !chip.ghost) {
      order[idx] = chip
      continue
    }
    if (!existing.ghost && chip.ghost) continue

    const chipRank = REACH_RANK[chip.reachability] ?? 9
    const existingRank = REACH_RANK[existing.reachability] ?? 9
    order[idx] = {
      ...existing,
      reachability: chipRank < existingRank ? chip.reachability : existing.reachability,
      planned: existing.planned || chip.planned,
      ghost: existing.ghost && chip.ghost,
    }
  }

  return order
}

export function chipsForNode(
  node: TopologyNode,
  roleView: RoleView,
  options?: { showGhostOverlay?: boolean },
): StackChipModel[] {
  const services = node.matrix_services ?? []
  const roles = roleView === 'compose' ? node.compose_roles : node.k3s_roles
  const chips = roles.map(r => buildChip(r, roleView, services, false))

  if (options?.showGhostOverlay) {
    const altRoles = roleView === 'compose' ? node.k3s_roles : node.compose_roles
    const existingIds = new Set(chips.map(c => c.componentId))
    for (const role of altRoles) {
      const componentId = roleToComponentId(role)
      if (!existingIds.has(componentId)) {
        chips.push(buildChip(role, roleView === 'compose' ? 'k3s' : 'compose', services, true))
        existingIds.add(componentId)
      }
    }
  }

  return mergeChipsByComponent(chips)
}

export function chipIdsMatchingTarget(
  topology: TopologyResponse,
  targetId: string,
  roleView?: RoleView,
): string[] {
  const views: RoleView[] = roleView ? [roleView] : ['compose', 'k3s']
  const seen = new Set<string>()
  const ids: string[] = []

  for (const view of views) {
    for (const node of topology.nodes) {
      for (const chip of chipsForNode(node, view)) {
        const match =
          chip.matrixTargetId === targetId ||
          (isTenantApiTarget(chip.componentId, targetId) &&
            node.matrix_services?.some(s => s.id === targetId))
        if (match && !seen.has(chip.chipId)) {
          seen.add(chip.chipId)
          ids.push(chip.chipId)
        }
      }
    }
  }
  return ids
}
