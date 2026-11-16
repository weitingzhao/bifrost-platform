import type { SystemDomainId } from '@/lib/architecture/systemDomainCatalog'
export function hasGoldenSignals(domain: SystemDomainId): boolean {
  return domain === 'satellite' || domain === 'ground-systems'
}
export const TENANT_ATTENTION_OWNERS = { satellite: 'Satellite', subcontractors: 'Subcontractors / IB Gateway' }
