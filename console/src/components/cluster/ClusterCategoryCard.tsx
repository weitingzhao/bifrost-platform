import type { LucideIcon } from 'lucide-react'
import { Copy, Check } from 'lucide-react'
import { StatusLamp } from '@/components/StatusLamp'
import type { Reachability } from '@/api/types'

export interface ClusterCategoryCardProps {
  title: string
  reach: Reachability
  headline: string
  detail?: string
  meta?: string
  icon?: LucideIcon
  loading?: boolean
  selected?: boolean
  copyState?: 'idle' | 'copied' | 'error'
  onSelect: () => void
  onCopyForLlm?: () => void
}

function reachToneClass(reach: Reachability, loading: boolean): string {
  if (loading) return ''
  switch (reach) {
    case 'ok':
      return ' cluster-category-card--ok'
    case 'degraded':
      return ' cluster-category-card--warn'
    case 'fail':
      return ' cluster-category-card--fail'
    default:
      return ' cluster-category-card--unknown'
  }
}

export function ClusterCategoryCard({
  title,
  reach,
  headline,
  detail,
  meta,
  icon: Icon,
  loading = false,
  selected = false,
  copyState = 'idle',
  onSelect,
  onCopyForLlm,
}: ClusterCategoryCardProps) {
  const toneClass = reachToneClass(reach, loading)
  const showDetail = detail != null && detail !== '' && !loading && reach !== 'ok'

  return (
    <div
      className={`cluster-category-card${toneClass}${selected ? ' cluster-category-card--selected' : ''}`}
    >
      {onCopyForLlm != null && !loading && (
        <button
          type="button"
          className="cluster-category-card__copy"
          aria-label={
            copyState === 'copied'
              ? 'Copied for LLM'
              : copyState === 'error'
                ? 'Copy failed'
                : `Copy ${title} for LLM`
          }
          title={
            copyState === 'copied'
              ? 'Copied!'
              : copyState === 'error'
                ? 'Copy failed'
                : 'Copy for LLM'
          }
          onClick={e => {
            e.stopPropagation()
            onCopyForLlm()
          }}
        >
          {copyState === 'copied' ? (
            <Check className="cluster-category-card__copy-icon" aria-hidden="true" />
          ) : (
            <Copy className="cluster-category-card__copy-icon" aria-hidden="true" />
          )}
        </button>
      )}
      <button
        type="button"
        className="cluster-category-card__select"
        onClick={onSelect}
        aria-pressed={selected}
        aria-current={selected ? 'true' : undefined}
      >
        <div className="cluster-category-card__head">
          {Icon != null && <Icon className="cluster-category-card__icon" aria-hidden="true" />}
          <StatusLamp
            value={loading ? 'unknown' : reach}
            kind="reach"
            variant={selected ? 'filled' : 'outline'}
          />
          <h4 className="cluster-category-card__title">{title}</h4>
          {selected && <span className="cluster-category-card__active">Active</span>}
        </div>
        <p className="cluster-category-card__headline">{loading ? 'Loading…' : headline}</p>
        {showDetail && <p className="cluster-category-card__detail">{detail}</p>}
        {meta != null && meta !== '' && !loading && (
          <p className="cluster-category-card__meta">{meta}</p>
        )}
      </button>
    </div>
  )
}
