import type { SimpleIcon } from 'simple-icons'
import {
  siArgo,
  siCelery,
  siDocker,
  siFastapi,
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

import type { Reachability, TopologyMatrixService, TopologyNode, TopologyResponse } from '@/api/types'
import type { ScopeTag } from '@/lib/runtime-map/runtimeMapRegistry'

export type RoleView = 'compose' | 'k3s'

export type InfraComponentVisual = {
  id: string
  label: string
  lettermark: string
  brandColor: string
  simpleIconSlug?: string
  scopeTag?: ScopeTag
  matrixTargets?: string[]
  plannedInK3s?: boolean
  plannedInCompose?: boolean
}

export type StackChipModel = {
  chipId: string
  componentId: string
  label: string
  lettermark: string
  brandColor: string
  planned: boolean
  ghost?: boolean
  reachability: Reachability
  matrixTargetId?: string
  scopeTag?: ScopeTag
}

const ICON_BY_COMPONENT: Record<string, SimpleIcon | undefined> = {
  postgres: siPostgresql,
  redis: siRedis,
  nginx: siNginx,
  docker: siDocker,
  k3s: siK3s,
  traefik: siTraefikproxy,
  gitea: siGitea,
  argocd: siArgo,
  tekton: siTekton,
  prometheus: siPrometheus,
  grafana: siGrafana,
  fastapi: siFastapi,
  platform: siGo,
  ollama: siOllama,
  celery: siCelery,
}

export const INFRA_COMPONENTS: Record<string, InfraComponentVisual> = {
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
  fastapi: {
    id: 'fastapi',
    label: 'Trade APIs',
    lettermark: 'API',
    brandColor: '#009688',
    simpleIconSlug: 'fastapi',
    scopeTag: 'TRADE-API',
  },
  platform: {
    id: 'platform',
    label: 'Platform API',
    lettermark: 'GO',
    brandColor: '#00ADD8',
    simpleIconSlug: 'go',
    scopeTag: 'PLATFORM',
    matrixTargets: ['api-ops', 'ops-capabilities'],
  },
  ib: {
    id: 'ib',
    label: 'IB TWS',
    lettermark: 'IB',
    brandColor: '#DB1222',
    scopeTag: 'TWS',
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
  celery: {
    id: 'celery',
    label: 'Celery',
    lettermark: 'CE',
    brandColor: '#37814a',
    simpleIconSlug: 'celery',
    scopeTag: 'WORKER',
    plannedInCompose: true,
  },
  socket: {
    id: 'socket',
    label: 'IB Socket',
    lettermark: 'SK',
    brandColor: '#0ea5e9',
    scopeTag: 'SOCKET',
    plannedInCompose: true,
  },
  frontend: {
    id: 'frontend',
    label: 'Frontend',
    lettermark: 'FE',
    brandColor: '#646cff',
    scopeTag: 'TRADE-FE',
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
export const SCOPE_DEFAULT_COMPONENT: Partial<Record<ScopeTag, string>> = {
  PLATFORM: 'platform',
  'TRADE-API': 'fastapi',
  INFRA: 'nginx',
  PG: 'postgres',
  REDIS: 'redis',
  K3S: 'k3s',
  GITHUB: 'gitea',
  OBSERVE: 'prometheus',
  AI: 'ollama',
  WORKER: 'celery',
  SOCKET: 'socket',
  'TRADE-FE': 'frontend',
  TWS: 'ib',
}

const ROLE_MAP: Array<{ match: (role: string) => boolean; componentId: string }> = [
  { match: r => r.includes('postgres') || r === 'cnpg', componentId: 'postgres' },
  { match: r => r === 'redis', componentId: 'redis' },
  { match: r => r === 'nginx' || r === 'ingress', componentId: 'nginx' },
  { match: r => r.includes('compose') || r === 'prod_compose' || r === 'dev_compose', componentId: 'docker' },
  { match: r => r.startsWith('k3s'), componentId: 'k3s' },
  { match: r => r === 'api', componentId: 'fastapi' },
  { match: r => r === 'gitea', componentId: 'gitea' },
  { match: r => r === 'argocd', componentId: 'argocd' },
  { match: r => r === 'tekton', componentId: 'tekton' },
  { match: r => r === 'monitoring', componentId: 'prometheus' },
  { match: r => r.includes('tws') || r.includes('ib'), componentId: 'ib' },
  { match: r => r === 'ollama', componentId: 'ollama' },
  { match: r => r === 'celery' || r.includes('socket'), componentId: 'socket' },
  { match: r => r === 'frontend', componentId: 'frontend' },
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
  if (componentId === 'ib') return undefined
  return ICON_BY_COMPONENT[componentId]?.path
}

export function scopeTagForComponent(componentId: string): ScopeTag | undefined {
  return INFRA_COMPONENTS[componentId]?.scopeTag
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
        (component.id === 'fastapi' && s.id.startsWith('api-') && s.id !== 'api-ops') ||
        (component.id === 'platform' && (s.id === 'api-ops' || s.id === 'ops-capabilities')),
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
  if (component.id === 'fastapi') {
    const fail = services.find(s => s.id.startsWith('api-') && s.id !== 'api-ops' && s.reachability === 'fail')
    if (fail) return fail.id
    return services.find(s => s.id.startsWith('api-') && s.id !== 'api-ops')?.id
  }
  if (component.id === 'platform') return 'api-ops'
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
          (targetId.startsWith('api-') &&
            chip.componentId === 'fastapi' &&
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

/** @deprecated use getComponentVisual */
export function getComponentDef(componentId: string): InfraComponentVisual {
  return getComponentVisual(componentId)
}
