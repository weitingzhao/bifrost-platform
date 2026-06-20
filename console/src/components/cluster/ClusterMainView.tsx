import { SegmentControl } from '@bifrost/ui'
import type { ReactNode } from 'react'
import {
  CLUSTER_VIEW_SECTIONS,
  type ClusterViewSection,
} from '@/lib/cluster/clusterViewSections'

interface ClusterMainViewProps {
  section: ClusterViewSection
  onSectionChange: (section: ClusterViewSection) => void
  nodes: ReactNode
  workloads: ReactNode
  platform: ReactNode
}

export function ClusterMainView({
  section,
  onSectionChange,
  nodes,
  workloads,
  platform,
}: ClusterMainViewProps) {
  const sectionMeta = CLUSTER_VIEW_SECTIONS.find(s => s.value === section)

  return (
    <section className="cluster-view-shell page-section panel-elevated overflow-visible">
      <header className="cluster-view-shell__header">
        <span className="cluster-view-shell__nav-label">Details</span>
        <SegmentControl
          value={section}
          onChange={v => onSectionChange(v as ClusterViewSection)}
          options={CLUSTER_VIEW_SECTIONS.map(s => ({ value: s.value, label: s.label }))}
          size="sm"
        />
        {sectionMeta?.hint != null && sectionMeta.hint !== '' && (
          <p className="cluster-view-shell__hint">{sectionMeta.hint}</p>
        )}
      </header>

      <div className="cluster-view-shell__body">
        {section === 'nodes' && <div className="cluster-view-panels">{nodes}</div>}
        {section === 'workloads' && <div className="cluster-view-panels">{workloads}</div>}
        {section === 'platform' && <div className="cluster-view-panels">{platform}</div>}
      </div>
    </section>
  )
}
