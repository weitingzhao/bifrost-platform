import { Button } from '@bifrost/ui'
import type { ClusterServiceReadinessResponse, Reachability, ServiceDomainStatus } from '@/api/types'
import { StatusLamp } from '@/components/StatusLamp'
import type { ClusterViewSection } from '@/lib/cluster/clusterViewSections'

interface ClusterServiceReadinessStripProps {
  data: ClusterServiceReadinessResponse | undefined
  isLoading?: boolean
  onNavigate: (section: ClusterViewSection) => void
}

function statusLabel(status: ServiceDomainStatus | string): string {
  switch (status) {
    case 'ready':
      return 'Ready'
    case 'partial':
      return 'Partial'
    case 'standby':
      return 'Standby'
    case 'unavailable':
      return 'Unavailable'
    default:
      return status
  }
}

function domainReach(status: ServiceDomainStatus | string, reachability: Reachability): Reachability {
  if (status === 'unavailable') return 'fail'
  if (status === 'ready' && reachability === 'ok') return 'ok'
  if (status === 'ready') return reachability
  if (status === 'partial' || status === 'standby') return 'degraded'
  return reachability
}

function pillClass(status: ServiceDomainStatus | string): string {
  const base = 'cluster-service-pill'
  if (status === 'unavailable') return `${base} cluster-service-pill--fail`
  if (status === 'partial' || status === 'standby') return `${base} cluster-service-pill--warn`
  if (status === 'ready') return `${base} cluster-service-pill--ok`
  return base
}

export function ClusterServiceReadinessStrip({
  data,
  isLoading = false,
  onNavigate,
}: ClusterServiceReadinessStripProps) {
  const domains = data?.domains ?? []
  const readyCount = domains.filter(d => d.status === 'ready').length
  const overallReach: Reachability = data?.reachability ?? 'unknown'

  return (
    <section className="cluster-service-strip">
      <div className="cluster-service-strip__head">
        <div className="cluster-service-strip__title-row">
          <StatusLamp value={isLoading ? 'unknown' : overallReach} kind="reach" />
          <h4 className="cluster-service-strip__title">Service readiness</h4>
          {!isLoading && domains.length > 0 && (
            <span className="cluster-service-strip__meta">
              {readyCount}/{domains.length} domains ready
            </span>
          )}
        </div>
        <Button variant="ghost" size="sm" className="h-7 px-2 text-[var(--text-dense-caption)]" onClick={() => onNavigate('platform')}>
          Details →
        </Button>
      </div>
      <p className="cluster-service-strip__desc">
        Application stack — PostgreSQL, Redis, workers, apps, and CI/CD. Distinct from K8s infrastructure below.
      </p>
      <div className="cluster-service-strip__pills">
        {isLoading ? (
          <span className="cluster-service-strip__loading">Loading domains…</span>
        ) : domains.length === 0 ? (
          <span className="cluster-service-strip__loading">Cluster unreachable</span>
        ) : (
          domains.map(domain => (
            <button
              key={domain.id}
              type="button"
              className={pillClass(domain.status)}
              title={domain.summary}
              onClick={() => onNavigate('platform')}
            >
              <StatusLamp value={domainReach(domain.status, domain.reachability)} kind="reach" />
              <span className="cluster-service-pill__label">{domain.label}</span>
              <span className="cluster-service-pill__status">{statusLabel(domain.status)}</span>
            </button>
          ))
        )}
      </div>
    </section>
  )
}
