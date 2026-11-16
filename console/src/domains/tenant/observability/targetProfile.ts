import type { SystemDomainId } from '@/lib/architecture/systemDomainCatalog'
export const TENANT_TARGET_HINTS: {match: RegExp; domain: SystemDomainId; role: 'required' | 'evidence'; envHint?: 'dev' | 'stg' | 'prod' | 'shared'}[] = [
{ match: /bifrost-dev|namespace="bifrost-dev"/i, domain: 'satellite', role: 'required', envHint: 'dev' },
{ match: /bifrost-stg|namespace="bifrost-stg"/i, domain: 'satellite', role: 'required', envHint: 'stg' },
{ match: /bifrost-prod|namespace="bifrost-prod"/i, domain: 'satellite', role: 'required', envHint: 'prod' },
{ match: /ib-gateway|ib_gateway/i, domain: 'subcontractors', role: 'required', envHint: 'shared' },
]
