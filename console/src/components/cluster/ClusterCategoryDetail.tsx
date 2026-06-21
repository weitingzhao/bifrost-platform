import type { ReactNode } from 'react'
import { Button } from '@bifrost/ui'
import {
  categoryDimension,
  INFRASTRUCTURE_CATEGORY_LABELS,
  isInfrastructureCategory,
} from '@/lib/cluster/clusterCategories'
import type { ClusterCategory } from '@/lib/cluster/clusterCategories'

type CopyState = 'idle' | 'copied' | 'error'

interface ClusterCategoryDetailProps {
  category: ClusterCategory | null
  title?: string
  copyState?: CopyState
  onCopyForLlm?: () => void
  applicationContent: (domainId: string) => ReactNode
  nodesContent: ReactNode
  workloadsContent: ReactNode
  governanceContent: ReactNode
  observabilityContent: ReactNode
}

function detailTitle(category: ClusterCategory, titleOverride?: string): string {
  if (titleOverride != null && titleOverride !== '') return titleOverride
  if (isInfrastructureCategory(category)) {
    return INFRASTRUCTURE_CATEGORY_LABELS[category]
  }
  return category
}

function copyLabel(state: CopyState): string {
  switch (state) {
    case 'copied':
      return 'Copied!'
    case 'error':
      return 'Copy failed'
    default:
      return 'Copy for LLM'
  }
}

export function ClusterCategoryDetail({
  category,
  title,
  copyState = 'idle',
  onCopyForLlm,
  applicationContent,
  nodesContent,
  workloadsContent,
  governanceContent,
  observabilityContent,
}: ClusterCategoryDetailProps) {
  if (category == null) return null

  let body: ReactNode = null
  if (categoryDimension(category) === 'application') {
    body = applicationContent(category)
  } else {
    switch (category) {
      case 'nodes':
        body = nodesContent
        break
      case 'workloads':
        body = workloadsContent
        break
      case 'governance':
        body = governanceContent
        break
      case 'observability':
        body = observabilityContent
        break
      default:
        body = null
    }
  }

  if (body == null) return null

  const resolvedTitle = detailTitle(category, title)

  return (
    <section className="cluster-category-detail page-section panel-elevated overflow-visible">
      <header className="cluster-category-detail__header">
        <div className="cluster-category-detail__heading">
          <span className="cluster-category-detail__label">Detail</span>
          <h3 className="cluster-category-detail__title">{resolvedTitle}</h3>
        </div>
        {onCopyForLlm != null && (
          <Button variant="outline" size="sm" onClick={() => onCopyForLlm()}>
            {copyLabel(copyState)}
          </Button>
        )}
      </header>
      <div className="cluster-category-detail__body">{body}</div>
    </section>
  )
}
