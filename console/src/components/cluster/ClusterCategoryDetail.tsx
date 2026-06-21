import type { ReactNode } from 'react'
import {
  categoryDimension,
  INFRASTRUCTURE_CATEGORY_LABELS,
  isInfrastructureCategory,
} from '@/lib/cluster/clusterCategories'
import type { ClusterCategory } from '@/lib/cluster/clusterCategories'

interface ClusterCategoryDetailProps {
  category: ClusterCategory | null
  title?: string
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

export function ClusterCategoryDetail({
  category,
  title,
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

  return (
    <section className="cluster-category-detail page-section panel-elevated overflow-visible">
      <header className="cluster-category-detail__header">
        <span className="cluster-category-detail__label">Detail</span>
        <h3 className="cluster-category-detail__title">{detailTitle(category, title)}</h3>
      </header>
      <div className="cluster-category-detail__body">{body}</div>
    </section>
  )
}
