import type { ScopeTag } from '@/domains/foundation/runtime-map/contracts'

export const tenantTargetScopeRule: { match: (id: string) => boolean; tag: ScopeTag } = {
  match: id => id.startsWith('api-'), tag: 'TRADE-API',
}
