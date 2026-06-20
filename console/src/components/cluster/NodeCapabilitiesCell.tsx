import { DenseTag } from '@bifrost/ui'
import type { ClusterNodeCapability } from '@/api/types'
import { capabilityTagVariant } from '@/lib/cluster/nodeCapabilitiesCatalog'

interface NodeCapabilitiesCellProps {
  capabilities?: ClusterNodeCapability[]
  /** Table cells use compact single-line tags; drawer uses full labels with tooltips. */
  compact?: boolean
}

export function NodeCapabilitiesCell({ capabilities, compact = true }: NodeCapabilitiesCellProps) {
  if (capabilities == null || capabilities.length === 0) {
    return <span className="text-[var(--muted-foreground)]">—</span>
  }

  return (
    <span className="inline-flex max-w-full flex-wrap items-center gap-0.5">
      {capabilities.map(cap => (
        <DenseTag
          key={cap.id}
          variant={capabilityTagVariant(cap.id)}
          title={cap.detail ?? cap.label}
          className={compact ? 'max-w-[7rem] truncate' : undefined}
        >
          {compact ? cap.label : cap.label}
        </DenseTag>
      ))}
    </span>
  )
}
