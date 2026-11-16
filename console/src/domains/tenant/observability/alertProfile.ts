import type { SystemDomainId } from '@/lib/architecture/systemDomainCatalog'
import type { AlertSeverity, ObservabilityEnvId } from '@/domains/foundation/observability/types'
export const TENANT_ALERT_RULES: { nameMatch: RegExp; domain: SystemDomainId; severity: AlertSeverity }[] = [
{ nameMatch: /Http5xx|ApiError|ApiLatency|TradeApi/i, domain: 'satellite', severity: 'critical' },
{ nameMatch: /IbGateway|IBGateway|TwsDisconnect/i, domain: 'subcontractors', severity: 'critical' },
]
export function tenantDomainFromNamespace(ns: string): SystemDomainId | null {
  if (/^bifrost-(dev|stg|prod)$/.test(ns)) return 'satellite'
  if (/^plugin-/.test(ns)) return 'subcontractors'
  return null
}
export function tenantEnvFromNamespace(ns: string): ObservabilityEnvId | null {
  if (ns === 'bifrost-dev') return 'dev'
  if (ns === 'bifrost-stg') return 'stg'
  if (ns === 'bifrost-prod') return 'prod'
  return null
}
export const TENANT_JOB_FAILED_RULE = { nameMatch: /^KubeJobFailed$/i, domain: 'subcontractors' as const, severity: 'warning' as const }
