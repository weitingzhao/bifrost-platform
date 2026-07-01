import type { ReactNode } from 'react'
import { DenseTag } from '@bifrost/ui'
import {
  GOVERNANCE_LAYER_CONSTITUTION,
  GOVERNANCE_LAYER_PROJECTION,
  GOVERNANCE_LAYER_SPINE,
} from '@/lib/architecture/blueprintCatalog'
import { blueprintZoneMeta } from '@/lib/architecture/blueprintZones'

type GovernanceLayer =
  | typeof GOVERNANCE_LAYER_CONSTITUTION
  | typeof GOVERNANCE_LAYER_SPINE
  | typeof GOVERNANCE_LAYER_PROJECTION

const ZONE_STYLES: Record<
  GovernanceLayer,
  { border: string; headerBg: string; tagVariant: 'category' | 'warning' | 'neutral' }
> = {
  [GOVERNANCE_LAYER_CONSTITUTION]: {
    border: 'border-[var(--primary)]/35',
    headerBg: 'bg-[var(--primary)]/8',
    tagVariant: 'category',
  },
  [GOVERNANCE_LAYER_SPINE]: {
    border: 'border-[var(--warning)]/40',
    headerBg: 'bg-[var(--warning)]/10',
    tagVariant: 'warning',
  },
  [GOVERNANCE_LAYER_PROJECTION]: {
    border: 'border-[var(--border)]',
    headerBg: 'bg-[var(--secondary)]/60',
    tagVariant: 'neutral',
  },
}

export function BlueprintGovernanceZone({
  layer,
  anchorId,
  children,
}: {
  layer: GovernanceLayer
  anchorId: string
  children: ReactNode
}) {
  const meta = blueprintZoneMeta(layer)
  const styles = ZONE_STYLES[layer]

  return (
    <section
      id={anchorId}
      className={`scroll-mt-20 flex flex-col gap-4 rounded-lg border-2 ${styles.border} p-3 md:p-4`}
    >
      <header
        className={`flex flex-wrap items-center gap-2 rounded-md px-3 py-2 ${styles.headerBg}`}
      >
        <DenseTag variant={styles.tagVariant}>{layer}</DenseTag>
        {meta != null && (
          <>
            <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              {meta.changeRate}
            </span>
            <span className="hidden text-[var(--text-dense-caption)] text-[var(--muted-foreground)] sm:inline">
              ·
            </span>
            <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              {meta.authority}
            </span>
          </>
        )}
      </header>
      <div className="flex flex-col gap-4">{children}</div>
    </section>
  )
}
