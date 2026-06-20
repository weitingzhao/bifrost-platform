import { StatusLamp } from '@/components/StatusLamp'
import type { Reachability } from '@/api/types'

export interface ClusterDimensionSummaryCardProps {
  title: string
  reach: Reachability
  headline: string
  detail?: string
  meta?: string
  loading?: boolean
  onDetails: () => void
}

export function ClusterDimensionSummaryCard({
  title,
  reach,
  headline,
  detail,
  meta,
  loading = false,
  onDetails,
}: ClusterDimensionSummaryCardProps) {
  return (
    <article className="cluster-dimension-card">
      <div className="cluster-dimension-card__head">
        <StatusLamp value={loading ? 'unknown' : reach} kind="reach" />
        <h4 className="cluster-dimension-card__title">{title}</h4>
      </div>
      <p className="cluster-dimension-card__headline">
        {loading ? 'Loading…' : headline}
      </p>
      {detail != null && detail !== '' && !loading && (
        <p className="cluster-dimension-card__detail">{detail}</p>
      )}
      {meta != null && meta !== '' && !loading && (
        <p className="cluster-dimension-card__meta">{meta}</p>
      )}
      <button type="button" className="cluster-dimension-card__link" onClick={onDetails}>
        Details →
      </button>
    </article>
  )
}
