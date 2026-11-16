import { siFastapi, siCelery } from 'simple-icons'
import type { InfraComponentVisual } from '@/domains/foundation/runtime-map/contracts'
import type { TopologyMatrixService } from '@/api/matrixTypes'
import type { ScopeTag } from '@/domains/foundation/runtime-map/contracts'

export const tenantComponents: Record<string, InfraComponentVisual> = {
  fastapi: {
    id: 'fastapi',
    label: 'Trade APIs',
    lettermark: 'API',
    brandColor: '#009688',
    simpleIconSlug: 'fastapi',
    scopeTag: 'TRADE-API',
  },
  ib: {
    id: 'ib',
    label: 'IB TWS',
    lettermark: 'IB',
    brandColor: '#DB1222',
    scopeTag: 'TWS',
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
}

export const tenantScopeComponents: Partial<Record<ScopeTag, string>> = {
  'TRADE-API': 'fastapi',
  WORKER: 'celery',
  SOCKET: 'socket',
  'TRADE-FE': 'frontend',
  TWS: 'ib',
}

export const tenantRoleRules = {
  api: { match: (r: string) => r === 'api', componentId: 'fastapi' },
  ib: { match: (r: string) => r.includes('tws') || r.includes('ib'), componentId: 'ib' },
  socket: { match: (r: string) => r === 'celery' || r.includes('socket'), componentId: 'socket' },
  frontend: { match: (r: string) => r === 'frontend', componentId: 'frontend' },
}

export function isTenantApiTarget(componentId: string, targetId: string): boolean {
  return componentId === 'fastapi' && targetId.startsWith('api-')
}

export function tenantMatrixTarget(componentId: string, services: TopologyMatrixService[]): string | undefined {
  if (componentId === 'fastapi') {
    const fail = services.find(s => s.id.startsWith('api-') && s.reachability === 'fail')
    if (fail) return fail.id
    return services.find(s => s.id.startsWith('api-'))?.id
  }
  return undefined
}

export const tenantIcons = { fastapi: siFastapi, celery: siCelery }

export function isTenantCustomIcon(componentId: string): boolean {
  return componentId === 'ib'
}

export function tenantEdgeDash(kind: string): string | undefined {
  if (kind === 'ib') return '2 4'
  return undefined
}
